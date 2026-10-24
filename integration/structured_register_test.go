package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/registrations"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func registrationButton(t *testing.T, h *harness, update, message, user int64, data string) uuid.UUID {
	t.Helper()
	u := telegram.Update{
		ID: update,
		CallbackQuery: &telegram.CallbackQuery{
			ID:      uuid.NewString(),
			From:    telegram.User{ID: user, LanguageCode: "en"},
			Message: &telegram.Message{ID: message, Chat: telegram.Chat{ID: user, Type: "private"}},
			Data:    data,
		},
	}
	raw, err := json.Marshal(u)
	require.NoError(t, err)
	task, _, err := h.service.Accept(t.Context(), u, raw)
	require.NoError(t, err)
	return task.ID
}

func TestStructuredRegisterEmptyPagesDeliverWithoutInlineKeyboard(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	for n, command := range []string{"/register", "/register after " + uuid.NewString()} {
		id := h.submitText(t, int64(92700+n), command)
		h.await(t, id, "completed")
		h.awaitReplies(t, n+1)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, message := range p.sent {
		require.Contains(t, message.Text, "No open registrations")
		keyboard, ok := message.ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
		require.True(t, ok)
		require.NotEmpty(t, keyboard.Keyboard)
	}
	require.Empty(t, p.runs)
}

func TestStructuredRegisterNoLoomRestartAndCanonicalReceipt(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19801, true)
	event := registrationEvent(t, h, admin)
	p.mu.Lock()
	p.adminRole = false
	p.mu.Unlock()
	list := h.submitText(t, 91800, "/register")
	h.await(t, list, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	keyboard := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	link := keyboard.InlineKeyboard[0][0]
	p.mu.Unlock()
	require.Equal(t, event.Document.Name, link.Text)
	prompt := registrationButton(t, h, 91801, 1, 456, link.CallbackData)
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	roles := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	button := roles.InlineKeyboard[0][0].CallbackData
	p.mu.Unlock()
	require.LessOrEqual(t, len(button), 64)
	h.stop(t)
	h.start(t)
	click := registrationButton(t, h, 91802, 2, 456, button)
	h.await(t, click, "completed")
	h.awaitReplies(t, 3)
	require.Equal(t, click, registrationButton(t, h, 91802, 2, 456, button))
	repeat := registrationButton(t, h, 91803, 2, 456, button)
	h.await(t, repeat, "completed")
	h.awaitReplies(t, 4)
	task, err := h.store.GetTask(t.Context(), click)
	require.NoError(t, err)
	user, err := h.store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	scope := db.EventScope{BotID: 123, Organization: user.Organization}
	result, err := h.store.RegistrationJoinOperation(
		t.Context(),
		scope,
		user.Subject,
		"register:"+prompt.String()+":leader",
	)
	require.NoError(t, err)
	require.Equal(t, registrations.Waitlisted, result.State)
	require.Nil(t, result.Pass)
	current, err := h.store.Registration(t.Context(), scope, event.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, result.ID, current.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Empty(t, p.inputs)
	require.Zero(t, p.loomUnavailableCalls)
	require.Contains(t, p.sent[2].Text, "waiting list")
}

func TestStructuredRegisterRejectsUnboundStaleAndForeignChoices(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19802, true)
	event := registrationEvent(t, h, admin)
	prompt := h.submitText(t, 91900, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	button := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0].CallbackData
	p.mu.Unlock()
	wrong := registrationButton(t, h, 91901, 999, 456, button)
	h.await(t, wrong, "completed")
	h.awaitReplies(t, 2)
	foreign := registrationButton(t, h, 91902, 1, 22002, button)
	h.await(t, foreign, "completed")
	h.awaitReplies(t, 3)
	changed := event.Document
	changed.Name = "Updated registration event"
	_, err := h.store.ChangeEvent(
		t.Context(),
		db.EventScope{BotID: 123, Organization: "org"},
		"admin-fixture",
		"edit-for-stale",
		db.EventChange{ID: event.ID, ExpectedVersion: event.Version, Document: changed},
	)
	require.NoError(t, err)
	stale := registrationButton(t, h, 91903, 1, 456, button)
	h.await(t, stale, "completed")
	h.awaitReplies(t, 4)
	task, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	_, err = h.store.Registration(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, event.ID, task.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	for _, sent := range p.sent[1:] {
		require.Contains(t, sent.Text, "could not be verified")
	}
}

func TestStructuredRegisterRecoversCommittedReceiptAfterChoiceExpiry(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19803, true)
	event := registrationEvent(t, h, admin)
	prompt := h.submitText(t, 92000, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	task, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	user, err := h.store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	scope := db.EventScope{BotID: 123, Organization: user.Organization}
	var saved struct {
		Entry db.RegistrationEntry `json:"registration_entry"`
	}
	require.NoError(t, json.Unmarshal(task.Snapshot, &saved))
	change := db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: user.ID, Role: "leader"}
	operation := "register:" + prompt.String() + ":leader"
	first, err := h.store.RegisterPresented(t.Context(), scope, user.Subject, operation, change, saved.Entry)
	require.NoError(t, err)
	// Model a successful commit before its task checkpoint, then the deadline passing.
	_, err = h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_tasks SET snapshot=jsonb_set(snapshot,'{registration_entr" +
"y,expires_at}',to_jsonb($2::text)) WHERE id=$1", prompt, time.Now().Add(-time.Second).UTC().Format(time.RFC3339Nano))
	require.NoError(t, err)
	h.stop(t)
	h.start(t)
	replay := registrationButton(t, h, 92001, 1, 456, "g:"+prompt.String()+":leader")
	h.await(t, replay, "completed")
	h.awaitReplies(t, 2)
	current, err := h.store.Registration(t.Context(), scope, event.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, first.ID, current.ID)
	other := registrationButton(t, h, 92002, 1, 456, "g:"+prompt.String()+":follower")
	h.await(t, other, "completed")
	h.awaitReplies(t, 3)
	unchanged, err := h.store.Registration(t.Context(), scope, event.ID, user.ID)
	require.NoError(t, err)
	require.Equal(t, "leader", unchanged.Role)
}

func TestStructuredRegisterSelectionReachesAgentAndUsesSameRegistration(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19804, true)
	event := registrationEvent(t, h, admin)
	prompt := h.submitText(t, 92100, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	task, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	user, err := h.store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	next := h.submitText(t, 92101, "Continue for the selected event as leader")
	h.await(t, next, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], `"selected_event"`)
	require.Contains(t, p.createdInputs[0], event.ID.String())
	token := p.token(user.Subject)
	p.mu.Unlock()
	agentRecord := registerForEvent(t, h, token, event.ID, "agent-continuation", "leader")
	choice := registrationButton(t, h, 92102, 1, 456, "g:"+prompt.String()+":leader")
	h.await(t, choice, "completed")
	h.awaitReplies(t, 3)
	current, err := h.store.Registration(
		t.Context(),
		db.EventScope{BotID: 123, Organization: user.Organization},
		event.ID,
		user.ID,
	)
	require.NoError(t, err)
	require.Equal(t, agentRecord.ID, current.ID)
	// A fresh expired guard cannot create another role request.
	_, err = h.store.RegisterPresented(
		t.Context(),
		db.EventScope{BotID: 123, Organization: user.Organization},
		user.Subject,
		"expired-fresh",
		db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: user.ID, Role: "follower"},
		db.RegistrationEntry{EventID: event.ID, EventVersion: event.Version, ExpiresAt: time.Now().Add(-time.Minute)},
	)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestStructuredRegisterExpiryIsCheckedAfterOwnerLock(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, admin := registrationUser(t, h, 19805, true)
	event := registrationEvent(t, h, admin)
	userID, _ := registrationUser(t, h, 19806, false)
	user, err := h.store.GetUser(t.Context(), userID)
	require.NoError(t, err)
	lock, err := h.store.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = lock.Rollback(t.Context()) }()
	_, err = lock.Exec(t.Context(), "SELECT id FROM shuttle_users WHERE id=$1 FOR UPDATE", userID)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	expiry := time.Now().UTC().Add(100 * time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, writeErr := h.store.RegisterPresented(
			ctx,
			db.EventScope{BotID: 123, Organization: user.Organization},
			user.Subject,
			"waiting-owner-expiry",
			db.RegistrationChange{Kind: "register", EventID: event.ID, UserID: user.ID, Role: "leader"},
			db.RegistrationEntry{EventID: event.ID, EventVersion: event.Version, ExpiresAt: expiry},
		)
		done <- writeErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := h.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND wait_event_type='Lock' AND query LIKE '%shuttle_users%')").
			Scan(&waiting)
		return queryErr == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool {
		var expired bool
		queryErr := h.store.Pool().QueryRow(t.Context(), "SELECT clock_timestamp()>=$1", expiry).Scan(&expired)
		return queryErr == nil && expired
	}, time.Second, 5*time.Millisecond)
	require.NoError(t, lock.Commit(t.Context()))
	select {
	case writeErr := <-done:
		require.ErrorIs(t, writeErr, db.ErrConflict)
	case <-ctx.Done():
		t.Fatal("Registration did not finish after releasing owner lock")
	}
	_, err = h.store.Registration(
		t.Context(),
		db.EventScope{BotID: 123, Organization: user.Organization},
		event.ID,
		user.ID,
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestStructuredRegisterUnknownPromptDeliveryDoesNotAuthorizeChoice(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19807, true)
	event := registrationEvent(t, h, admin)
	prompt := h.submitText(t, 92200, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	_, err := h.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_outbox SET state='delivery_unknown' WHERE task_id=$1", prompt)
	require.NoError(t, err)
	choice := registrationButton(t, h, 92201, 1, 456, "g:"+prompt.String()+":leader")
	h.await(t, choice, "completed")
	h.awaitReplies(t, 2)
	task, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	_, err = h.store.Registration(t.Context(), db.EventScope{BotID: 123, Organization: "org"}, event.ID, task.UserID)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Contains(t, p.sent[1].Text, "could not be verified")
}

func TestStructuredRegisterUsesProfileLanguageForSavedRole(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19808, true)
	event := registrationEvent(t, h, admin)
	prompt := h.submitText(t, 92300, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 1)
	task, err := h.store.GetTask(t.Context(), prompt)
	require.NoError(t, err)
	mutation := profileChange("set_language", 0)
	mutation.BotLanguage = "ru"
	changeProfile(t, h, ownerProfileScope(t, h, task.UserID), mutation)
	choice := registrationButton(t, h, 92301, 1, 456, "g:"+prompt.String()+":follower")
	h.await(t, choice, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[1].Text, "Роль: Фолловер")
	require.Empty(t, p.runs)
}

func TestStructuredRegisterPreservesPendingAgentQuestion(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	h := setup(t, p)
	_, admin := registrationUser(t, h, 19809, true)
	event := registrationEvent(t, h, admin)
	original := h.submitText(t, 92400, "Ask me a question")
	h.await(t, original, "awaiting_user")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	prompt := h.submitText(t, 92401, "/register "+event.ID.String())
	h.await(t, prompt, "completed")
	h.awaitReplies(t, 2)
	choice := registrationButton(t, h, 92402, 2, 456, "g:"+prompt.String()+":leader")
	h.await(t, choice, "completed")
	h.awaitReplies(t, 3)
	h.stop(t)
	h.start(t)
	p.mu.Lock()
	require.Empty(t, p.inputs)
	p.loomDown = false
	p.mu.Unlock()
	answer := h.submitText(t, 92403, "My actual answer")
	h.await(t, answer, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 4)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Empty(t, p.inputs[0].QuestionID)
	require.Contains(t, p.inputs[0].Text, `"selected_event"`)
	require.Contains(t, p.inputs[0].Text, event.ID.String())
}
