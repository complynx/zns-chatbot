package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

type knowledgeHTTPFixture struct {
	h                         *harness
	p                         *peers
	owner, reviewer, other    appSession
	ownerScope, reviewerScope db.PaymentScope
}

type knowledgeHTTPPreparation struct {
	OperationID   string          `json:"operation_id"`
	Arguments     json.RawMessage `json:"arguments"`
	Preconditions struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
}

func knowledgeHTTPScope(t *testing.T, h *harness, session appSession) db.PaymentScope {
	t.Helper()
	u, err := h.store.GetUser(t.Context(), session.UserID)
	require.NoError(t, err)
	return db.PaymentScope{BotID: u.BotID, Organization: u.Organization, UserID: u.ID, Subject: u.Subject}
}

func newKnowledgeHTTPFixture(t *testing.T) knowledgeHTTPFixture {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	f := knowledgeHTTPFixture{
		h:        h,
		p:        p,
		owner:    detailsAppSession(t, h, 47101, 101),
		reviewer: detailsAppSession(t, h, 47102, 102),
		other:    detailsAppSession(t, h, 47103, 103),
	}
	f.ownerScope = knowledgeHTTPScope(t, h, f.owner)
	f.reviewerScope = knowledgeHTTPScope(t, h, f.reviewer)
	// Trusted fixture provisioning grants only the reviewer a persisted moderator assignment.
	admin := f.ownerScope
	admin.GlobalAdmin = true
	enabled := true
	knowledgeApply(
		t,
		knowledgeFixture{h: h},
		admin,
		db.KnowledgeChange{
			Kind:             "set_moderator",
			ModeratorUserID:  f.reviewer.UserID,
			ModeratorEnabled: &enabled,
			Reason:           "Provision independent HTTP reviewer",
		},
	)
	return f
}

func knowledgeHTTPBody(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func knowledgeNativeCall(
	t *testing.T,
	f knowledgeHTTPFixture,
	session appSession,
	method string,
	body any,
	expected int,
) []byte {
	t.Helper()
	code, raw, headers := appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/knowledge/"+method,
		knowledgeHTTPBody(t, body),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, expected, code, string(raw))
	require.Equal(t, "no-store", headers.Get("Cache-Control"))
	return raw
}

func knowledgeAgentCall(
	t *testing.T,
	f knowledgeHTTPFixture,
	scope db.PaymentScope,
	method string,
	body any,
	expected int,
) []byte {
	t.Helper()
	f.p.mu.Lock()
	token := f.p.token(scope.Subject)
	f.p.mu.Unlock()
	code, raw := adminRequest(t, f.h, token, http.MethodPost, "/internal/agent/knowledge/"+method, body)
	require.Equal(t, expected, code, string(raw))
	return raw
}

func knowledgeHTTPPrepare(t *testing.T, raw []byte) knowledgeHTTPPreparation {
	t.Helper()
	var prep knowledgeHTTPPreparation
	require.NoError(t, json.Unmarshal(raw, &prep))
	require.NotEqual(t, uuid.Nil, prep.Preconditions.ProposalID)
	require.NotEmpty(t, prep.Arguments)
	return prep
}

func knowledgeNativeConfirmation(prep knowledgeHTTPPreparation, confirmed bool) map[string]any {
	return map[string]any{
		"operation_id": prep.OperationID,
		"arguments":    prep.Arguments,
		"proposal_id":  prep.Preconditions.ProposalID,
		"confirmed":    confirmed,
	}
}

func TestKnowledgeHTTPNativeSuggestionAgentReviewAndRecovery(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	content := knowledgeContent()
	content.Body = "# HTTP reference\nNative proposal body canary."
	op := uuid.NewString()
	prep := knowledgeHTTPPrepare(
		t,
		knowledgeNativeCall(
			t,
			f,
			f.owner,
			"suggest/prepare",
			map[string]any{
				"operation_id": op,
				"arguments":    map[string]any{"content": content, "reason": "Suggest through the native form"},
			},
			http.StatusOK,
		),
	)
	recent := knowledgeNativeCall(t, f, f.owner, "recent_actions", struct{}{}, http.StatusOK)
	require.Contains(t, string(recent), op)
	require.NotContains(t, string(recent), "Native proposal body canary")
	other := knowledgeNativeCall(t, f, f.other, "recent_actions", struct{}{}, http.StatusOK)
	require.NotContains(t, string(other), op)
	other = knowledgeNativeCall(t, f, f.other, "suggest/status", map[string]any{"operation_id": op}, http.StatusOK)
	require.Contains(t, string(other), `"status":"unknown"`)
	require.NotContains(t, string(other), "Native proposal body canary")
	submitted := knowledgeNativeCall(
		t,
		f,
		f.owner,
		"suggest/confirm",
		knowledgeNativeConfirmation(prep, true),
		http.StatusOK,
	)
	var result struct {
		Suggestion struct {
			ID        uuid.UUID `json:"id"`
			ArticleID uuid.UUID `json:"article_id"`
		} `json:"suggestion"`
	}
	require.NoError(t, json.Unmarshal(submitted, &result))
	require.NotEqual(t, uuid.Nil, result.Suggestion.ID)
	knowledgeNativeCall(
		t,
		f,
		f.owner,
		"review/prepare",
		map[string]any{
			"operation_id": uuid.NewString(),
			"arguments": map[string]any{
				"suggestion_id": result.Suggestion.ID,
				"decision":      "approve",
				"reason":        "Cannot review my own proposal",
			},
		},
		http.StatusForbidden,
	)
	reviewOp := uuid.NewString()
	review := knowledgeHTTPPrepare(
		t,
		knowledgeAgentCall(
			t,
			f,
			f.reviewerScope,
			"review/prepare",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     reviewOp,
				"arguments": map[string]any{
					"suggestion_id":     result.Suggestion.ID,
					"expected_revision": 0,
					"decision":          "approve",
					"reason":            "Independently checked HTTP proposal",
				},
			},
			http.StatusOK,
		),
	)
	status := knowledgeNativeCall(
		t,
		f,
		f.reviewer,
		"review/status",
		map[string]any{"operation_id": reviewOp},
		http.StatusOK,
	)
	require.Contains(t, string(status), review.Preconditions.ProposalID.String())
	approved := knowledgeNativeCall(
		t,
		f,
		f.reviewer,
		"review/confirm",
		knowledgeNativeConfirmation(review, true),
		http.StatusOK,
	)
	f.h.stop(t)
	f.h.start(t)
	replay := knowledgeNativeCall(
		t,
		f,
		f.reviewer,
		"review/confirm",
		knowledgeNativeConfirmation(review, true),
		http.StatusOK,
	)
	require.JSONEq(t, string(approved), string(replay))
	article := knowledgeNativeCall(
		t,
		f,
		f.owner,
		"read",
		map[string]any{"article_id": result.Suggestion.ArticleID},
		http.StatusOK,
	)
	var read struct {
		Body string `json:"body"`
	}
	require.NoError(t, json.Unmarshal(article, &read))
	require.Equal(t, content.Body, read.Body)
}

func TestKnowledgeHTTPAgentPreparationRequiresExactNativeConsent(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	content := knowledgeContent()
	content.Slug = "http-agent-reference"
	op := uuid.NewString()
	prep := knowledgeHTTPPrepare(
		t,
		knowledgeAgentCall(
			t,
			f,
			f.reviewerScope,
			"edit/prepare",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     op,
				"arguments": map[string]any{
					"content":           content,
					"expected_revision": 0,
					"reason":            "Prepare with the agent API",
				},
			},
			http.StatusOK,
		),
	)
	recent := knowledgeNativeCall(t, f, f.reviewer, "recent_actions", struct{}{}, http.StatusOK)
	require.Contains(t, string(recent), op)
	original := knowledgeNativeCall(t, f, f.reviewer, "edit/status", map[string]any{"operation_id": op}, http.StatusOK)
	var status struct {
		Preparation knowledgeHTTPPreparation `json:"preparation"`
	}
	require.NoError(t, json.Unmarshal(original, &status))
	require.JSONEq(t, string(prep.Arguments), string(status.Preparation.Arguments))
	require.Equal(t, prep.Preconditions, status.Preparation.Preconditions)
	knowledgeNativeCall(
		t,
		f,
		f.reviewer,
		"edit/confirm",
		knowledgeNativeConfirmation(prep, false),
		http.StatusBadRequest,
	)
	var changed map[string]any
	require.NoError(t, json.Unmarshal(prep.Arguments, &changed))
	changed["reason"] = "A different change"
	wrong := knowledgeNativeConfirmation(prep, true)
	wrong["arguments"] = changed
	knowledgeNativeCall(t, f, f.reviewer, "edit/confirm", wrong, http.StatusConflict)
	knowledgeNativeCall(t, f, f.reviewer, "edit/confirm", knowledgeNativeConfirmation(prep, true), http.StatusOK)
	operation := knowledgeAgentCall(
		t,
		f,
		f.reviewerScope,
		"operation",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.NewString(),
			"arguments":        map[string]any{"kind": "edit", "operation_id": op},
		},
		http.StatusOK,
	)
	var observed struct {
		Result struct {
			State       string `json:"state"`
			OperationID string `json:"operation_id"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(operation, &observed))
	require.Equal(t, "completed", observed.Result.State)
	require.Equal(t, op, observed.Result.OperationID)
}

func TestKnowledgeHTTPRejectsForeignOriginsRevocationAndInvalidPreparation(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	for _, origin := range []string{"", "https://foreign.example.invalid"} {
		code, _, _ := appCall(t, f.h, http.MethodPost, "/app/knowledge/recent_actions", "{}", f.owner.Token, origin)
		require.Equal(t, http.StatusForbidden, code)
	}
	code, _, _ := appCall(t, f.h, http.MethodPost, "/app/knowledge/recent_actions", "{}", "", appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code)
	content := knowledgeContent()
	content.Slug = "invalid slug"
	op := uuid.NewString()
	knowledgeNativeCall(
		t,
		f,
		f.owner,
		"suggest/prepare",
		map[string]any{
			"operation_id": op,
			"arguments":    map[string]any{"content": content, "reason": "Invalid draft remains editable"},
		},
		http.StatusBadRequest,
	)
	unknown := knowledgeNativeCall(t, f, f.owner, "suggest/status", map[string]any{"operation_id": op}, http.StatusOK)
	require.Contains(t, string(unknown), `"status":"unknown"`)
	content.Slug = "valid-after-rejection"
	prep := knowledgeHTTPPrepare(
		t,
		knowledgeNativeCall(
			t,
			f,
			f.reviewer,
			"edit/prepare",
			map[string]any{
				"operation_id": uuid.NewString(),
				"arguments": map[string]any{
					"content": content,
					"reason":  "Current permission must be checked again",
				},
			},
			http.StatusOK,
		),
	)
	admin := f.ownerScope
	admin.GlobalAdmin = true
	disabled := false
	knowledgeApply(
		t,
		knowledgeFixture{h: f.h},
		admin,
		db.KnowledgeChange{
			Kind:                     "set_moderator",
			ModeratorUserID:          f.reviewer.UserID,
			ModeratorEnabled:         &disabled,
			ExpectedModeratorVersion: 1,
			Reason:                   "Revoke reviewer before confirmation",
		},
	)
	knowledgeNativeCall(t, f, f.reviewer, "edit/confirm", knowledgeNativeConfirmation(prep, true), http.StatusForbidden)
	var args struct {
		ArticleID uuid.UUID `json:"article_id"`
	}
	require.NoError(t, json.Unmarshal(prep.Arguments, &args))
	knowledgeNativeCall(t, f, f.owner, "read", map[string]any{"article_id": args.ArticleID}, http.StatusNotFound)
	code, raw, _ := appCall(t, f.h, http.MethodPost, "/app/session/logout", "{}", f.owner.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, _, _ = appCall(
		t,
		f.h,
		http.MethodPost,
		"/app/knowledge/suggest/status",
		knowledgeHTTPBody(t, map[string]any{"operation_id": op}),
		f.owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}
