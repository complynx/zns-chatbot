package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMiniAppOrderReviewContextIsBoundedRedactedAndPermissionFiltered(t *testing.T) {
	t.Parallel()
	f, scope, review, session := nativeOrderReviewWriteFixture(t)
	profile := ownerProfileScope(t, f.H, f.AmbassadorID)
	before := paymentReviewHistoryTask(t, f, 99100)
	agent, err := f.H.store.PrepareOrderReviewWorkspace(
		t.Context(),
		scope,
		uuid.New(),
		db.OrderReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	agentTask := paymentReviewHistoryTask(t, f, 99101)
	found, err := f.H.store.HasMiniAppOrderReviewActions(t.Context(), profile, agentTask.ID)
	require.NoError(t, err)
	require.False(t, found)
	review.Reason = "PRIVATE_ORDER_REVIEW_REASON"
	revision := agent.Revision
	for range 7 {
		w := saveOrderReviewForm(t, f, session, uuid.New(), revision, review)
		revision = w.Revision
	}
	after := paymentReviewHistoryTask(t, f, 99102)
	rows, err := f.H.store.RecentMiniAppOrderReviewActions(t.Context(), scope, before.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	found, err = f.H.store.HasMiniAppOrderReviewActions(t.Context(), profile, after.ID)
	require.NoError(t, err)
	require.True(t, found)
	rows, err = f.H.store.RecentMiniAppOrderReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for i, row := range rows {
		require.Equal(t, revision-int64(i), row.Revision)
		require.Equal(t, "prepared", row.Outcome)
	}
	raw, err := json.Marshal(rows)
	require.NoError(t, err)
	for _, private := range []string{review.Reason, "amount_minor", "file_id", session.Token, agent.ID.String()} {
		require.NotContains(t, string(raw), private)
	}
	foreign := paymentReviewHistoryTask(t, f, 99103)
	ownerScope := paymentScope(t, f.H, f.OwnerID, false)
	rows, err = f.H.store.RecentMiniAppOrderReviewActions(t.Context(), ownerScope, foreign.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	wrong := scope
	wrong.Organization = "other-org"
	_, err = f.H.store.RecentMiniAppOrderReviewActions(t.Context(), wrong, after.ID)
	require.Error(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2`, f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	rows, err = f.H.store.RecentMiniAppOrderReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	scope.GlobalAdmin = true
	rows, err = f.H.store.RecentMiniAppOrderReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_events SET modules=ARRAY['registrations'] WHERE id=$1`, f.Event.ID)
	require.NoError(t, err)
	rows, err = f.H.store.RecentMiniAppOrderReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestMiniAppOrderReviewContextRetainsOriginalOutcomesAfterReplayAndRestart(t *testing.T) {
	t.Parallel()
	f, scope, review, session := nativeOrderReviewWriteFixture(t)
	save := uuid.New()
	w := saveOrderReviewForm(t, f, session, save, 0, review)
	cancel := uuid.New()
	code, raw := orderReviewFormCall(
		t,
		f,
		session,
		"cancellations",
		map[string]any{"operation_id": cancel, "workspace_id": w.ID, "revision": w.Revision},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	w = saveOrderReviewForm(t, f, session, uuid.New(), w.Revision, review)
	commit := uuid.New()
	body := orderReviewConfirmBody(commit, w)
	code, raw = orderReviewFormCall(t, f, session, "confirmations", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	before := paymentReviewHistoryTask(t, f, 99200)
	original, err := f.H.store.RecentMiniAppOrderReviewActions(t.Context(), scope, before.ID)
	require.NoError(t, err)
	require.Len(t, original, 4)
	require.Equal(t, "confirmed", original[0].Outcome)
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	code, raw = orderReviewFormCall(t, f, session, "confirmations", body)
	require.Equal(t, http.StatusOK, code, string(raw))
	code, raw = orderReviewFormCall(
		t,
		f,
		session,
		"workspaces",
		map[string]any{"operation_id": save, "expected_revision": 0, "language": "en", "review": review},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	after := paymentReviewHistoryTask(t, f, 99201)
	rows, err := reopened.RecentMiniAppOrderReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Equal(t, original, rows)
	outcomes := map[string]int{}
	for _, row := range rows {
		outcomes[row.Outcome]++
	}
	require.Equal(t, map[string]int{"prepared": 2, "cancelled": 1, "confirmed": 1}, outcomes)
	require.Equal(t, int64(1), orderAllocationUnits(t, f, "occupied"))
}

func TestMiniAppOrderReviewContextReachesOnlyExplicitAgentTurn(t *testing.T) {
	t.Parallel()
	f, _, review, session := nativeOrderReviewWriteFixture(t)
	review.Reason = "PRIVATE_ORDER_REVIEW_REASON"
	w := saveOrderReviewForm(t, f, session, uuid.New(), 0, review)
	code, raw := orderReviewFormCall(t, f, session, "confirmations", orderReviewConfirmBody(uuid.New(), w))
	require.Equal(t, http.StatusOK, code, string(raw))
	p := f.H.peers
	p.mu.Lock()
	initial := len(p.runs)
	p.mu.Unlock()
	require.Zero(t, initial)
	f.H.stop(t)
	f.H.start(t)
	accepted := paymentReviewMessage(t, f.H, 99300, 11003, "Continue the order payment decision I made in the form")
	f.H.await(t, accepted, "completed")
	p.mu.Lock()
	prompt := p.createdInputs[len(p.createdInputs)-1]
	runs := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, 1, runs)
	require.Contains(t, prompt, "Recent completed Mini App order payment review requests")
	require.Contains(t, prompt, w.ID.String())
	require.Contains(t, prompt, `"outcome":"confirmed"`)
	require.Contains(t, prompt, "zinusya_order_payments.review_workspace")
	for _, private := range []string{review.Reason, session.Token, "amount_minor"} {
		require.NotContains(t, prompt, private)
	}
	_, err := f.H.store.Pool().
		Exec(t.Context(), `DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2`, f.Event.ID, f.AmbassadorID)
	require.NoError(t, err)
	accepted = paymentReviewMessage(t, f.H, 99301, 11003, "Check what I can review now")
	f.H.await(t, accepted, "completed")
	p.mu.Lock()
	prompt = p.createdInputs[len(p.createdInputs)-1]
	p.mu.Unlock()
	require.NotContains(t, prompt, "Recent completed Mini App order payment review requests")
	require.NotContains(t, prompt, w.ID.String())
}
