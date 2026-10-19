package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/payments"
)

const paymentAppPath = "/app/payment-reviews/"

func paymentAppFixture(t *testing.T) (paymentFixture, payments.ReviewChange, appSession) {
	t.Helper()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.fileBytes = []byte("%PDF-1.4\nprivate native payment proof")
	f := setupPayment(t, p)
	stopContext, stopCancel := context.WithTimeout(t.Context(), time.Second)
	require.NoError(t, f.H.service.Stop(stopContext))
	stopCancel()
	f.H.service = nil
	file := paymentFile(t, f.H, f.OwnerID, 19201, "native-api-proof", time.Now().UTC().Truncate(time.Second))
	evidence := submittedPayment(t, f, "native-api-evidence", file, 4000, payments.Initial)
	review := payments.ReviewChange{
		EventID:     f.Event.ID,
		EvidenceID:  evidence.ID,
		Decision:    payments.Confirmed,
		AmountMinor: 3087,
	}
	return f, review, detailsAppSession(t, f.H, 11003, 71)
}

func paymentAppJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	return string(raw)
}

func paymentAppSaved(
	t *testing.T,
	f paymentFixture,
	session appSession,
	review payments.ReviewChange,
	revision int64,
) db.PaymentReviewWorkspace {
	t.Helper()
	body := paymentAppJSON(
		t,
		map[string]any{"operation_id": uuid.New(), "expected_revision": revision, "review": review},
	)
	code, raw, _ := appCall(t, f.H, http.MethodPost, paymentAppPath+"workspaces", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var w db.PaymentReviewWorkspace
	require.NoError(t, json.Unmarshal(raw, &w))
	return w
}

func paymentAppConfirmBody(t *testing.T, w db.PaymentReviewWorkspace) string {
	t.Helper()
	return paymentAppJSON(
		t,
		map[string]any{
			"operation_id":          uuid.New(),
			"canonical_proposal_id": w.Preparation.ID,
			"arguments":             workspaceCommit(w),
			"confirmed":             true,
		},
	)
}

func paymentAppEvidencePath(review payments.ReviewChange) string {
	return paymentAppPath + "events/" + review.EventID.String() + "/evidence/" + review.EvidenceID.String()
}

func TestMiniAppPaymentReviewHTTPHandoffAndLostReply(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	var tasksBefore int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_tasks`).Scan(&tasksBefore))
	code, raw, _ := appCall(t, f.H, http.MethodGet, paymentAppPath+"events", "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var choices events.ContextList
	require.NoError(t, json.Unmarshal(raw, &choices))
	require.Len(t, choices.Events, 1)
	require.Equal(t, f.Event.ID, choices.Events[0].ID)
	require.Equal(t, "Europe/Minsk", choices.Events[0].Timezone)
	path := paymentAppEvidencePath(review)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path, "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"workspace":null`)
	w := paymentAppSaved(t, f, session, review, 0)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	agent, err := f.H.store.PreparePaymentReviewCommit(t.Context(), scope, "loom-approved-alias", workspaceCommit(w))
	require.NoError(t, err)
	// Native writes need no Loom request; the worker is stopped and its mock is closed.
	f.H.peers.loom.Close()
	confirm := paymentAppConfirmBody(t, w)
	code, first, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		confirm,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(first))
	code, retry, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		confirm,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(retry))
	require.JSONEq(t, string(first), string(retry))
	var result db.PaymentWorkspaceReviewResult
	require.NoError(t, json.Unmarshal(first, &result))
	require.EqualValues(t, 3087, result.Review.Balance.ConfirmedMinor)
	recovered, err := f.H.store.CommitPaymentReviewWorkspace(
		t.Context(),
		scope,
		"loom-approved-alias",
		workspaceCommit(w),
		agent.ID,
	)
	require.NoError(t, err)
	require.Equal(t, result, recovered)
	var input struct {
		OperationID uuid.UUID `json:"operation_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(confirm), &input))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		paymentAppPath+"operations/"+input.OperationID.String(),
		"",
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"completed":true`)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path, "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"confirmed"`)
	var tasks int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), `SELECT count(*) FROM shuttle_tasks`).Scan(&tasks))
	require.Equal(t, tasksBefore, tasks, "native review must not start unsolicited agent work")
}

func TestMiniAppPaymentReviewHTTPValidationAndAccess(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	valid := map[string]any{"operation_id": uuid.New(), "expected_revision": 0, "review": review}
	cases := []string{
		`{}`,
		`null`,
		paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "review": review}),
		paymentAppJSON(
			t,
			map[string]any{
				"operation_id":      uuid.New(),
				"expected_revision": 0,
				"review": map[string]any{
					"event_id":     review.EventID,
					"evidence_id":  review.EvidenceID,
					"decision":     "confirmed",
					"amount_minor": 3087,
				},
			},
		),
		strings.TrimSuffix(paymentAppJSON(t, valid), "}") + `,"global_admin":true}`,
		paymentAppJSON(t, valid) + ` {}`,
	}
	for _, body := range cases {
		code, raw, _ := appCall(
			t,
			f.H,
			http.MethodPost,
			paymentAppPath+"workspaces",
			body,
			session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code, string(raw))
	}
	code, _, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"workspaces",
		paymentAppJSON(t, valid),
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code)
	owner := detailsAppSession(t, f.H, 11002, 72)
	code, raw, _ := appCall(t, f.H, http.MethodGet, paymentAppPath+"events", "", owner.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var choices events.ContextList
	require.NoError(t, json.Unmarshal(raw, &choices))
	require.Empty(t, choices.Events)
	for _, path := range []string{paymentAppEvidencePath(review), paymentAppEvidencePath(review) + "/content", paymentAppPath + "events/" + review.EventID.String() + "/evidence"} {
		code, raw, _ = appCall(t, f.H, http.MethodGet, path, "", owner.Token, appTestOrigin)
		require.Equal(t, http.StatusForbidden, code, string(raw))
		require.NotContains(t, string(raw), "%PDF")
	}
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"workspaces",
		paymentAppJSON(t, valid),
		owner.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	w := paymentAppSaved(t, f, session, review, 0)
	body := paymentAppConfirmBody(t, w)
	var confirm map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &confirm))
	confirm["confirmed"] = false
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		paymentAppJSON(t, confirm),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		paymentAppPath+"operations/"+uuid.NewString(),
		"",
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusNotFound, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, "/app/session/logout", `{}`, session.Token, appTestOrigin)
	require.Equal(t, http.StatusNoContent, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodPost, paymentAppPath+"confirmations", body, session.Token, appTestOrigin)
	require.Equal(t, http.StatusUnauthorized, code, string(raw))
}

func TestMiniAppPaymentReviewHTTPCancelAndAgentSavedIntent(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	w, err := f.H.store.PreparePaymentReviewWorkspace(
		t.Context(),
		scope,
		"agent-saved-intent",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	code, raw, _ := appCall(t, f.H, http.MethodGet, paymentAppEvidencePath(review), "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), w.ID.String())
	cancel := paymentAppJSON(
		t,
		map[string]any{"operation_id": uuid.New(), "workspace_id": w.ID, "revision": w.Revision},
	)
	code, first, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"cancellations",
		cancel,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(first))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"cancellations",
		cancel,
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.JSONEq(t, string(first), string(raw))
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		paymentAppConfirmBody(t, w),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code, string(raw))
	review.Decision = payments.Rejected
	review.AmountMinor = 0
	review.Reason = "proof belongs to another payment"
	replacement := paymentAppSaved(t, f, session, review, 1)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		paymentAppConfirmBody(t, replacement),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"state":"rejected"`)
}

func TestMiniAppPaymentReviewHTTPProofRevocationDuringIO(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"event-role", "session-logout"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, review, session := paymentAppFixture(t)
			operator := paymentScope(t, f.H, f.AdminID, true)
			done := make(chan error, 1)
			f.H.peers.mu.Lock()
			f.H.peers.fileReadHook = func() {
				if kind == "event-role" {
					_, err := f.H.store.ChangeAuthority(
						context.Background(),
						operator.EventScope,
						operator.Subject,
						"revoke-during-native-proof",
						db.AuthorityChange{
							EventID:         f.Event.ID,
							UserID:          f.AmbassadorID,
							Role:            db.AuthorityNone,
							ExpectedVersion: 1,
						},
						uuid.Nil,
					)
					done <- err
				} else {
					code, raw, _, err := appHTTPCall(
						f.H,
						http.MethodPost,
						"/app/session/logout",
						`{}`,
						session.Token,
						appTestOrigin,
					)
					if err == nil && code != http.StatusNoContent {
						err = fmt.Errorf("logout status %d: %s", code, raw)
					}
					done <- err
				}
			}
			f.H.peers.mu.Unlock()
			code, raw, _ := appCall(
				t,
				f.H,
				http.MethodGet,
				paymentAppEvidencePath(review)+"/content",
				"",
				session.Token,
				appTestOrigin,
			)
			expected := http.StatusForbidden
			if kind == "session-logout" {
				expected = http.StatusUnauthorized
			}
			require.Equal(t, expected, code, string(raw))
			require.NotContains(t, string(raw), "%PDF")
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("proof IO hook did not finish")
			}
		})
	}
}

func TestMiniAppPaymentReviewHTTPProofBoundsAndOwnerIsolation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"download", "gone", "too-large"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f, review, session := paymentAppFixture(t)
			f.H.peers.mu.Lock()
			f.H.peers.fileGone = kind == "gone"
			f.H.peers.fileTooLarge = kind == "too-large"
			f.H.peers.mu.Unlock()
			code, raw, headers := appCall(
				t,
				f.H,
				http.MethodGet,
				paymentAppEvidencePath(review)+"/content",
				"",
				session.Token,
				appTestOrigin,
			)
			switch kind {
			case "download":
				require.Equal(t, http.StatusOK, code, string(raw))
				require.Contains(t, string(raw), "%PDF")
				require.Equal(t, "attachment", headers.Get("Content-Disposition"))
				require.Equal(t, "private, no-store", headers.Get("Cache-Control"))
				require.Equal(t, "nosniff", headers.Get("X-Content-Type-Options"))
			case "gone":
				require.Equal(t, http.StatusServiceUnavailable, code, string(raw))
				require.NotContains(t, string(raw), "%PDF")
			case "too-large":
				require.Equal(t, http.StatusRequestEntityTooLarge, code, string(raw))
				require.NotContains(t, string(raw), "%PDF")
			}
			evidence, err := f.H.store.PaymentEvidence(
				t.Context(),
				paymentScope(t, f.H, f.AmbassadorID, false),
				review.EventID,
				review.EvidenceID,
			)
			require.NoError(t, err)
			code, raw = adminRequest(
				t,
				f.H,
				f.Ambassador,
				http.MethodGet,
				"/internal/files/"+evidence.FileID.String()+"/content",
				nil,
			)
			require.Equal(t, http.StatusNotFound, code, string(raw))
		})
	}
}

func TestMiniAppPaymentReviewHTTPVerifiedGlobalRole(t *testing.T) {
	t.Parallel()
	f, review, _ := paymentAppFixture(t)
	f.H.peers.mu.Lock()
	f.H.peers.adminRole = true
	f.H.peers.mu.Unlock()
	admin := detailsAppSession(t, f.H, 11001, 73)
	code, raw, _ := appCall(t, f.H, http.MethodGet, paymentAppEvidencePath(review), "", admin.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw, _ = appCall(t, f.H, http.MethodGet, paymentAppPath+"events", "", admin.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), f.Event.ID.String())
}

func TestMiniAppPaymentReviewHTTPPayerDisplayAndAccess(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	profile := `{"first_name":"Telegram","last_name":"Fallback","username":"not_disclosed","private":"not_disclosed"}`
	_, err := f.H.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET profile=$2 WHERE id=$1`, f.OwnerID, profile)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `INSERT INTO shuttle_user_profiles(user_id,version,full_name) VALUES($1,1,'Chosen payer') ON CONFLICT(user_id) DO UPDATE SET full_name=EXCLUDED.full_name`, f.OwnerID)
	require.NoError(t, err)
	eventPath := paymentAppPath + "events/" + review.EventID.String()
	code, raw, _ := appCall(t, f.H, http.MethodGet, eventPath, "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusOK, code, string(raw))
	var event events.ContextEvent
	require.NoError(t, json.Unmarshal(raw, &event))
	require.Equal(t, f.Event.ID, event.ID)
	require.Equal(t, f.Event.Document.Timezone, event.Timezone)
	path := paymentAppEvidencePath(review)
	read := func(want string) {
		t.Helper()
		statusCode, payload, _ := appCall(t, f.H, http.MethodGet, path, "", session.Token, appTestOrigin)
		require.Equal(t, http.StatusOK, statusCode, string(payload))
		var detail struct {
			Payer map[string]any `json:"payer"`
		}
		require.NoError(t, json.Unmarshal(payload, &detail))
		require.Equal(t, map[string]any{"user_id": f.OwnerID.String(), "full_name": want}, detail.Payer)
		require.NotContains(t, string(payload), "not_disclosed")
	}
	read("Chosen payer")
	_, err = f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_user_profiles SET full_name='' WHERE user_id=$1`, f.OwnerID)
	require.NoError(t, err)
	read("Telegram Fallback")
	_, err = f.H.store.Pool().Exec(t.Context(), `UPDATE shuttle_users SET profile='{}' WHERE id=$1`, f.OwnerID)
	require.NoError(t, err)
	read("")
	owner := detailsAppSession(t, f.H, 11002, 92)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path, "", owner.Token, appTestOrigin)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), `"payer"`)
	code, raw, _ = appCall(t, f.H, http.MethodGet, eventPath, "", owner.Token, appTestOrigin)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), event.Name)

	_, err = f.H.store.Pool().
		Exec(t.Context(), `DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2`, f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	code, raw, _ = appCall(t, f.H, http.MethodGet, path, "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), `"payer"`)
	code, raw, _ = appCall(t, f.H, http.MethodGet, eventPath, "", session.Token, appTestOrigin)
	require.Equal(t, http.StatusForbidden, code, string(raw))
	require.NotContains(t, string(raw), event.Name)
}
