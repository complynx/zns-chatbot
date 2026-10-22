package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredNavigationNeedsNoLoomAndReplaysAcceptedUpdates(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	for n, command := range []string{"/menu", "/profile", "/registrations", "/registrations invalid"} {
		id := int64(89100 + n)
		taskID := h.submitText(t, id, command)
		require.Equal(t, taskID, h.submitText(t, id, command))
		h.await(t, taskID, "completed")
		h.awaitReplies(t, n+1)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.loomUnavailableCalls)
	require.Empty(t, p.runs)
	require.Empty(t, p.inputs)
	require.Contains(t, p.sent[0].Text, "Zinusya\n/profile")
	require.Contains(t, p.sent[1].Text, "Name: Not set")
	require.Contains(t, p.sent[2].Text, "No registrations")
	require.Contains(t, p.sent[3].Text, "Open /registrations again")
	for _, sent := range p.sent {
		keyboard, ok := sent.ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
		require.True(t, ok)
		require.True(t, keyboard.ResizeKeyboard)
		require.Equal(t, "/profile", keyboard.Keyboard[0][1].Text)
	}
}

func TestStructuredNavigationPreservesAgentQuestionAcrossLoomOutageAndRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	original := h.submitText(t, 89200, "Ask me a question")
	h.await(t, original, "awaiting_user")
	h.awaitReplies(t, 1)
	active, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	user, err := h.store.GetUser(t.Context(), active.UserID)
	require.NoError(t, err)
	scope := db.ProfileScope{BotID: 123, Organization: user.Organization, Subject: user.Subject, UserID: user.ID}
	profile, err := h.store.MutateProfile(
		t.Context(),
		scope,
		db.ProfileMutation{OperationID: uuid.New(), Kind: "request_name"},
	)
	require.NoError(t, err)
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	for n, command := range []string{"/menu", "/profile", "/registrations"} {
		id := h.submitText(t, int64(89201+n), command)
		h.await(t, id, "completed")
		h.awaitReplies(t, n+2)
	}
	unchanged, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, profile.Version, unchanged.Version)
	require.Equal(t, profile.NameExpectation, unchanged.NameExpectation)
	h.stop(t)
	h.start(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	answer := h.submitText(t, 89205, "My answer")
	h.await(t, answer, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 5)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Empty(t, p.inputs[0].QuestionID)
	require.Equal(t, "My answer", promptUserText(p.inputs[0].Text))
	require.Contains(t, p.inputs[0].Text, "Recent manual navigation requests")
	require.Contains(t, p.inputs[0].Text, `"command":"/profile"`)
}

func TestStructuredNavigationRoutesAheadWithoutLosingNormalRoutingOrder(t *testing.T) {
	t.Parallel()
	h := prepareHarness(t, newPeers(t))
	accept := func(update int64, kind, text string) db.Task {
		payload, err := json.Marshal(map[string]string{"kind": kind, "text": text, "structured_command": text})
		require.NoError(t, err)
		task, _, err := h.store.Intake(t.Context(), db.IntakeRequest{BotID: 123, UpdateID: update, TelegramUserID: 456,
			ChatID: 456, Payload: payload}, nil)
		require.NoError(t, err)
		return task
	}
	first := accept(89300, "message", "First")
	menu := accept(89301, "structured", "/menu")
	second := accept(89302, "message", "Second")
	profile := accept(89303, "structured", "/profile")
	for _, id := range []uuid.UUID{profile.ID, menu.ID} {
		routed, err := h.store.Route(t.Context(), id)
		require.NoError(t, err)
		require.Equal(t, "structured", routed.Route)
		require.Equal(t, uuid.Nil, routed.TargetTaskID)
	}
	_, err := h.store.Route(t.Context(), second.ID)
	require.ErrorIs(t, err, db.ErrRoutingPending)
	routedFirst, err := h.store.Route(t.Context(), first.ID)
	require.NoError(t, err)
	require.Equal(t, "new_run", routedFirst.Route)
	routedSecond, err := h.store.Route(t.Context(), second.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, routedSecond.TargetTaskID)
	third := accept(89304, "message", "Third")
	routedThird, err := h.store.Route(t.Context(), third.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, routedThird.TargetTaskID)
}

func TestStructuredProfilePlainTextUsesSavedLanguageAndCurrentAuthorization(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	first := h.submitText(t, 89400, "/profile")
	h.await(t, first, "completed")
	h.awaitReplies(t, 1)
	task, err := h.store.GetTask(t.Context(), first)
	require.NoError(t, err)
	user, err := h.store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	scope := db.ProfileScope{BotID: 123, Organization: user.Organization, Subject: user.Subject, UserID: user.ID}
	profile, err := h.store.MutateProfile(t.Context(), scope, db.ProfileMutation{
		OperationID: uuid.New(), Kind: "set_name", FullName: "[Daniel](https://example.invalid)"})
	require.NoError(t, err)
	_, err = h.store.MutateProfile(t.Context(), scope, db.ProfileMutation{OperationID: uuid.New(), Kind: "set_language",
		ExpectedVersion: profile.Version, BotLanguage: "ru"})
	require.NoError(t, err)
	view := h.submitText(t, 89401, "/profile")
	h.await(t, view, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	require.Contains(t, p.sent[1].Text, "Мой профиль\nИмя: [Daniel](https://example.invalid)")
	require.Contains(t, p.sent[1].Text, "/name — Изменить имя")
	require.Empty(t, p.sent[1].Entities)
	p.mu.Unlock()
	_, err = h.store.Pool().Exec(context.Background(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", user.ID)
	require.NoError(t, err)
	denied := h.submitText(t, 89402, "/profile")
	h.await(t, denied, "failed")
	h.awaitReplies(t, 3)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotContains(t, p.sent[2].Text, "Daniel")
	require.Contains(t, p.sent[2].Text, "Access denied")
	require.Zero(t, p.loomUnavailableCalls)
}

func TestStructuredNavigationAcceptedDuringIAMOutageRecoversAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.iamDown, p.loomDown = true, true
	h := setup(t, p)
	id := h.submitText(t, 89500, "/menu")
	h.await(t, id, "awaiting_authorization")
	h.stop(t)
	p.mu.Lock()
	p.iamDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Zero(t, p.loomUnavailableCalls)
	require.Len(t, p.sent, 1)
}

func TestStructuredRegistrationShowsOwnedPassDepositAndBalanceWithoutLoom(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	f := setupPayment(t, p)
	for range 5 {
		event := registrationEvent(t, f.H, f.Admin)
		registerForEvent(t, f.H, f.Owner, event.ID, uuid.NewString(), "leader")
	}
	update := telegram.Update{ID: 89600, Message: &telegram.Message{
		ID: 89600,
		From: &telegram.User{
			ID:           11002,
			LanguageCode: "en",
		},
		Chat: telegram.Chat{ID: 11002, Type: "private"},
		Text: "/registrations",
	}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := f.H.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	f.H.await(t, accepted.ID, "completed")
	f.H.awaitReplies(t, 1)
	p.mu.Lock()
	first := p.sent[0]
	p.mu.Unlock()
	keyboard, ok := first.ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.True(t, ok)
	button := keyboard.InlineKeyboard[0][0]
	require.Equal(t, "Next page", button.Text)
	require.LessOrEqual(t, len(button.CallbackData), 64)
	callback := telegram.Update{
		ID: 89601,
		CallbackQuery: &telegram.CallbackQuery{ID: "qa-pagination", From: *update.Message.From,
			Message: &telegram.Message{ID: 1, Chat: update.Message.Chat}, Data: button.CallbackData},
	}
	raw, err = json.Marshal(callback)
	require.NoError(t, err)
	next, _, err := f.H.service.Accept(t.Context(), callback, raw)
	require.NoError(t, err)
	f.H.await(t, next.ID, "completed")
	f.H.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 2)
	require.EqualValues(t, 11002, p.sent[0].ChatID)
	require.EqualValues(t, 11002, p.sent[1].ChatID)
	require.Equal(t, []string{"qa-pagination"}, p.callbackAcks)
	text := strings.Join([]string{p.sent[0].Text, p.sent[1].Text}, "\n")
	require.Contains(t, text, f.Event.Document.Name)
	require.Contains(t, text, "Pass assigned")
	require.Contains(t, text, "Confirmed: 0.00 BYN")
	require.Contains(t, text, "Remaining: 123.45 BYN")
	require.Contains(t, text, "Remaining to secure deposit: 30.87 BYN")
	require.Contains(t, text, "Deposit due:")
	require.Contains(t, text, "Full payment due:")
	require.Contains(t, text, "("+f.Event.Document.Timezone+")")
	require.NotContains(t, text, "Paid in full")
	require.Zero(t, p.loomUnavailableCalls)
}
