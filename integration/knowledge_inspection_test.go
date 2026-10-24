package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

type inspectedKnowledgeOperation struct {
	Status           string          `json:"status"`
	OperationID      string          `json:"operation_id"`
	ContractRevision string          `json:"contract_revision"`
	Arguments        json.RawMessage `json:"arguments"`
	ExpiresAt        time.Time       `json:"expires_at"`
	Preconditions    struct {
		ProposalID uuid.UUID `json:"proposal_id"`
	} `json:"preconditions"`
	Binding struct {
		Subject string `json:"subject"`
		Actor   string `json:"actor"`
		OrgID   string `json:"org_id"`
	} `json:"binding"`
	Result json.RawMessage `json:"result"`
}

func inspectKnowledgeHTTP(
	t *testing.T,
	f knowledgeHTTPFixture,
	scope db.PaymentScope,
	op, kind string,
	expected int,
) []byte {
	t.Helper()
	f.p.mu.Lock()
	token := f.p.token(scope.Subject)
	f.p.mu.Unlock()
	code, raw := adminRequest(
		t,
		f.h,
		token,
		http.MethodPost,
		"/internal/agent/operations/inspect",
		map[string]any{"protocol_version": 1, "operation_id": op, "module": "zinusya_knowledge", "method": kind},
	)
	require.Equal(t, expected, code, string(raw))
	return raw
}

func TestKnowledgeInspectorPreservesOriginalNativeAndAgentPreparations(t *testing.T) {
	t.Parallel()
	for _, origin := range []string{"native", "agent"} {
		t.Run(origin, func(t *testing.T) {
			t.Parallel()
			f := newKnowledgeHTTPFixture(t)
			op := uuid.NewString()
			request := map[string]any{
				"operation_id": op,
				"arguments": map[string]any{
					"expected_revision": 0,
					"content":           knowledgeContent(),
					"reason":            "Continue the same reviewed proposal",
				},
			}
			var prepared []byte
			if origin == "native" {
				prepared = knowledgeNativeCall(t, f, f.owner, "suggest/prepare", request, http.StatusOK)
			} else {
				request["protocol_version"] = 1
				prepared = knowledgeAgentCall(t, f, f.ownerScope, "suggest/prepare", request, http.StatusOK)
			}
			prep := knowledgeHTTPPrepare(t, prepared)
			raw := inspectKnowledgeHTTP(t, f, f.ownerScope, op, "suggest", http.StatusOK)
			var found inspectedKnowledgeOperation
			require.NoError(t, json.Unmarshal(raw, &found))
			require.Equal(t, "prepared", found.Status)
			require.Equal(t, op, found.OperationID)
			require.Equal(t, "mvp-93", found.ContractRevision)
			require.Equal(t, prep.Preconditions.ProposalID, found.Preconditions.ProposalID)
			require.JSONEq(t, string(prep.Arguments), string(found.Arguments))
			require.Equal(t, f.ownerScope.Subject, found.Binding.Subject)
			require.Equal(t, "actor", found.Binding.Actor)
			require.Equal(t, "org", found.Binding.OrgID)
			firstExpiry := found.ExpiresAt
			receipt := knowledgeNativeCall(
				t,
				f,
				f.owner,
				"suggest/confirm",
				knowledgeNativeConfirmation(prep, true),
				http.StatusOK,
			)
			raw = inspectKnowledgeHTTP(t, f, f.ownerScope, op, "suggest", http.StatusOK)
			require.NoError(t, json.Unmarshal(raw, &found))
			require.Equal(t, "succeeded", found.Status)
			require.Equal(t, firstExpiry, found.ExpiresAt)
			require.JSONEq(t, string(receipt), string(found.Result))
			var preparations, effects int
			require.NoError(
				t,
				f.h.store.Pool().
					QueryRow(t.Context(), "SELECT (SELECT count(*) FROM shuttle_knowledge_preparations WHERE operat" +
"ion_id=$1),(SELECT count(*) FROM shuttle_knowledge_operations WHERE oper" +
"ation_id=$1)", op).
					Scan(&preparations, &effects),
			)
			require.Equal(t, 1, preparations)
			require.Equal(t, 1, effects)
		})
	}
}

func TestKnowledgeInspectorDoesNotRenewExpiredOrRelabelLegacyPreparation(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	op := uuid.NewString()
	knowledgeNativeCall(
		t,
		f,
		f.owner,
		"suggest/prepare",
		map[string]any{
			"operation_id": op,
			"arguments": map[string]any{
				"expected_revision": 0,
				"content":           knowledgeContent(),
				"reason":            "Check immutable terms",
			},
		},
		http.StatusOK,
	)
	var expired time.Time
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), "UPDATE shuttle_knowledge_preparations SET created_at=clock_timestamp()-i" +
"nterval '10 seconds',expires_at=clock_timestamp()-interval '1 second' WH" +
"ERE operation_id=$1 RETURNING expires_at", op).
			Scan(&expired),
	)
	raw := inspectKnowledgeHTTP(t, f, f.ownerScope, op, "suggest", http.StatusOK)
	var found inspectedKnowledgeOperation
	require.NoError(t, json.Unmarshal(raw, &found))
	require.Equal(t, "prepared", found.Status)
	require.True(t, expired.Equal(found.ExpiresAt), "Loom must receive the original expired deadline and fail closed")
	foreign := knowledgeHTTPScope(t, f.h, f.other)
	inspectKnowledgeHTTP(t, f, foreign, op, "suggest", http.StatusNotFound)
	inspectKnowledgeHTTP(t, f, f.ownerScope, op, "edit", http.StatusNotFound)
	_, err := f.h.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_knowledge_preparations SET document=document-'contract' WHERE operation_id=$1`, op)
	require.NoError(t, err)
	inspectKnowledgeHTTP(t, f, f.ownerScope, op, "suggest", http.StatusConflict)
	var document string
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), `SELECT document::text FROM shuttle_knowledge_preparations WHERE operation_id=$1`, op).
			Scan(&document),
	)
	require.NotContains(t, document, `"contract"`)
}

func TestKnowledgeInspectorRechecksCurrentModeratorAuthority(t *testing.T) {
	t.Parallel()
	f := newKnowledgeHTTPFixture(t)
	op := uuid.NewString()
	knowledgeAgentCall(
		t,
		f,
		f.reviewerScope,
		"edit/prepare",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     op,
			"arguments": map[string]any{
				"expected_revision": 0,
				"content":           knowledgeContent(),
				"reason":            "Publish a moderator reference",
			},
		},
		http.StatusOK,
	)
	inspectKnowledgeHTTP(t, f, f.reviewerScope, op, "edit", http.StatusOK)
	_, err := f.h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_knowledge_moderators SET enabled=false,version=version+1 " +
"WHERE user_id=$1", f.reviewerScope.UserID)
	require.NoError(t, err)
	raw := inspectKnowledgeHTTP(t, f, f.reviewerScope, op, "edit", http.StatusForbidden)
	require.NotContains(t, string(raw), knowledgeContent().Body)
	var effects int
	require.NoError(
		t,
		f.h.store.Pool().
			QueryRow(t.Context(), `SELECT count(*) FROM shuttle_knowledge_operations WHERE operation_id=$1`, op).
			Scan(&effects),
	)
	require.Zero(t, effects)
}
