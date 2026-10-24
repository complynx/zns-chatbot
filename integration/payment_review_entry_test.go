package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func paymentReviewMessage(t *testing.T, h *harness, id, tgID int64, text string) uuid.UUID {
	t.Helper()
	update := telegram.Update{
		ID: id,
		Message: &telegram.Message{
			ID:   id,
			From: &telegram.User{ID: tgID, LanguageCode: "en"},
			Chat: telegram.Chat{ID: tgID, Type: "private"},
			Text: text,
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	return accepted.ID
}

func TestStructuredPaymentReviewEntryUsesCurrentRightsWithoutLoom(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.loomDown = true
	f := setupPayment(t, p)
	for n, step := range []struct {
		user int64
		text string
	}{{11003, "/payment_reviews"}, {11003, "/menu"}, {11002, "/payment_reviews"}, {11002, "/menu"}} {
		accepted := paymentReviewMessage(t, f.H, 97500+int64(n), step.user, step.text)
		f.H.await(t, accepted, "completed")
		f.H.awaitReplies(t, n+1)
	}
	p.mu.Lock()
	sent := append([]telegram.SendMessageRequest(nil), p.sent...)
	runs := len(p.runs)
	unavailable := p.loomUnavailableCalls
	p.mu.Unlock()
	button := sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0]
	require.Equal(t, "Review payments", button.Text)
	require.Equal(t, appTestOrigin+"/app/#payment-review", button.WebApp.URL)
	require.Empty(t, button.CallbackData)
	require.Contains(t, sent[1].Text, "/payment_reviews")
	require.Contains(t, sent[2].Text, "requires current event reviewer access")
	require.NotContains(t, sent[3].Text, "/payment_reviews")
	require.Zero(t, runs)
	require.Zero(t, unavailable)
	user, err := f.H.store.GetUser(t.Context(), f.OwnerID)
	require.NoError(t, err)
	p.mu.Lock()
	p.adminRole = true
	p.mu.Unlock()
	f.H.iam.Invalidate(user.Subject)
	accepted := paymentReviewMessage(t, f.H, 97504, 11002, "/payment_reviews")
	f.H.await(t, accepted, "completed")
	f.H.awaitReplies(t, 5)
	p.mu.Lock()
	adminEntry := p.sent[4]
	p.mu.Unlock()
	require.IsType(t, &telegram.InlineKeyboardMarkup{}, adminEntry.ReplyMarkup)
	p.mu.Lock()
	p.adminRole = false
	p.mu.Unlock()
	f.H.iam.Invalidate(user.Subject)
	accepted = paymentReviewMessage(t, f.H, 97505, 11002, "/payment_reviews")
	f.H.await(t, accepted, "completed")
	f.H.awaitReplies(t, 6)
	p.mu.Lock()
	revoked := p.sent[5]
	p.mu.Unlock()
	require.Contains(t, revoked.Text, "requires current event reviewer access")
}

func TestStructuredPaymentReviewEntryPreservesAgentQuestionAcrossRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.question = true
	f := setupPayment(t, p)
	original := paymentReviewMessage(t, f.H, 97600, 11003, "Ask a question")
	f.H.await(t, original, "awaiting_user")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	entry := paymentReviewMessage(t, f.H, 97601, 11003, "/payment_reviews")
	f.H.await(t, entry, "completed")
	f.H.awaitReplies(t, 2)
	waiting, err := f.H.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	require.Equal(t, "awaiting_user", waiting.State)
	f.H.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	f.H.start(t)
	answer := paymentReviewMessage(t, f.H, 97602, 11003, "Continue the earlier question")
	f.H.await(t, answer, "completed")
	f.H.await(t, original, "completed")
	p.mu.Lock()
	runs := len(p.runs)
	inputs := append([]string(nil), p.createdInputs...)
	count := len(p.inputs)
	text := ""
	if count > 0 {
		text = p.inputs[0].Text
	}
	unavailable := p.loomUnavailableCalls
	p.mu.Unlock()
	require.Equal(t, 1, runs)
	require.Len(t, inputs, 1)
	require.Equal(t, 1, count)
	require.Contains(t, text, `"command":"/payment_reviews"`)
	require.Zero(t, unavailable)
}

func paymentReviewHistoryTask(t *testing.T, f paymentFixture, update int64) db.Task {
	t.Helper()
	accepted, _, err := f.H.store.Intake(
		t.Context(),
		db.IntakeRequest{
			BotID:          123,
			UpdateID:       update,
			TelegramUserID: 11003,
			ChatID:         11003,
			Payload:        json.RawMessage(`{"kind":"message","text":"Inspect saved review"}`),
		},
		nil,
	)
	require.NoError(t, err)
	return accepted
}

func TestMiniAppPaymentReviewContextIsBoundedRedactedAndPermissionFiltered(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	scope := paymentScope(t, f.H, f.AmbassadorID, false)
	before := paymentReviewHistoryTask(t, f, 97700)
	agent, err := f.H.store.PreparePaymentReviewWorkspace(
		t.Context(),
		scope,
		"agent-only-review",
		db.PaymentReviewDraftChange{Review: review},
	)
	require.NoError(t, err)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodGet,
		paymentAppPath+"operations/agent-only-review",
		"",
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	agentOnlyTask := paymentReviewHistoryTask(t, f, 97702)
	profile := ownerProfileScope(t, f.H, f.AmbassadorID)
	found, err := f.H.store.HasMiniAppPaymentReviewActions(t.Context(), profile, agentOnlyTask.ID)
	require.NoError(t, err)
	require.False(t, found)
	revision := agent.Revision
	review.Reason = "PRIVATE_REASON_MUST_NOT_ENTER_AGENT_CONTEXT"
	for range 7 {
		w := paymentAppSaved(t, f, session, review, revision)
		revision = w.Revision
	}
	after := paymentReviewHistoryTask(t, f, 97703)
	found, err = f.H.store.HasMiniAppPaymentReviewActions(t.Context(), profile, before.ID)
	require.NoError(t, err)
	require.False(t, found)
	rows, err := f.H.store.RecentMiniAppPaymentReviewActions(t.Context(), scope, before.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	rows, err = f.H.store.RecentMiniAppPaymentReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	for i, row := range rows {
		require.Equal(t, revision-int64(i), row.Revision)
	}
	raw, err = json.Marshal(rows)
	require.NoError(t, err)
	require.NotContains(t, string(raw), review.Reason)
	require.NotContains(t, string(raw), "amount_minor")
	require.NotContains(t, string(raw), "agent-only-review")
	require.NotContains(t, string(raw), session.Token)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2`, f.Event.ID,
			f.AmbassadorID)
	require.NoError(t, err)
	rows, err = f.H.store.RecentMiniAppPaymentReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
	scope.GlobalAdmin = true
	rows, err = f.H.store.RecentMiniAppPaymentReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Len(t, rows, 5)
	_, err = f.H.store.Pool().
		Exec(t.Context(), `UPDATE shuttle_events SET modules=ARRAY['orders'] WHERE id=$1`, f.Event.ID)
	require.NoError(t, err)
	rows, err = f.H.store.RecentMiniAppPaymentReviewActions(t.Context(), scope, after.ID)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestMiniAppPaymentReviewActionsReachOnlyExplicitAgentTurn(t *testing.T) {
	t.Parallel()
	f, review, session := paymentAppFixture(t)
	review.Reason = "PRIVATE_REVIEW_REASON"
	w := paymentAppSaved(t, f, session, review, 0)
	code, raw, _ := appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"cancellations",
		paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "workspace_id": w.ID, "revision": w.Revision}),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	w = paymentAppSaved(t, f, session, review, 1)
	code, raw, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		paymentAppPath+"confirmations",
		paymentAppConfirmBody(t, w),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	p := f.H.peers
	p.mu.Lock()
	initial := len(p.runs)
	p.mu.Unlock()
	require.Zero(t, initial)
	f.H.stop(t)
	f.H.start(t)
	accepted := paymentReviewMessage(t, f.H, 97800, 11003, "Continue the decision I made in the form")
	f.H.await(t, accepted, "completed")
	p.mu.Lock()
	prompt := p.createdInputs[len(p.createdInputs)-1]
	runs := len(p.runs)
	p.mu.Unlock()
	require.Equal(t, 1, runs)
	require.Contains(t, prompt, "Recent completed Mini App payment review requests")
	require.Contains(t, prompt, w.ID.String())
	require.Contains(t, prompt, `"outcome":"confirmed"`)
	require.Contains(t, prompt, `"outcome":"cancelled"`)
	require.NotContains(t, prompt, review.Reason)
	require.NotContains(t, prompt, "amount_minor")
	require.NotContains(t, prompt, session.Token)
	_, err := f.H.store.Pool().
		Exec(t.Context(), `DELETE FROM shuttle_event_authorities WHERE event_id=$1 AND user_id=$2`, f.Event.ID,
			f.AmbassadorID)
	require.NoError(t, err)
	accepted = paymentReviewMessage(t, f.H, 97801, 11003, "Check what I can review now")
	f.H.await(t, accepted, "completed")
	p.mu.Lock()
	prompt = p.createdInputs[len(p.createdInputs)-1]
	p.mu.Unlock()
	require.NotContains(t, prompt, "Recent completed Mini App payment review requests")
	require.NotContains(t, prompt, w.ID.String())
}

func TestPaymentReviewNotificationHasOnlyProtectedReadLink(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	f := setupPayment(t, p)
	evidence, _ := pendingNotice(t, f, "review-entry-notice")
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.sent) > 0 },
		5*time.Second,
		10*time.Millisecond,
	)
	p.mu.Lock()
	sent := append([]telegram.SendMessageRequest(nil), p.sent...)
	p.mu.Unlock()
	button := sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0]
	require.Equal(
		t,
		appTestOrigin+"/app/#payment-review?event_id="+f.Event.ID.String()+"&evidence_id="+evidence.ID.String(),
		button.WebApp.URL,
	)
	require.Empty(t, button.CallbackData)
	balance, err := f.H.store.PaymentBalance(t.Context(), paymentScope(t, f.H, f.OwnerID, false), f.Event.ID, f.Pass.ID)
	require.NoError(t, err)
	require.Zero(t, balance.ConfirmedMinor)
}
