package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func availabilityIntake(t *testing.T, store *db.Store, update int64) (db.Task, bool) {
	t.Helper()
	task, duplicate, err := store.Intake(t.Context(), db.IntakeRequest{BotID: 1, UpdateID: update,
		TelegramUserID: 42, ChatID: 42, Payload: json.RawMessage(`{"kind":"message"}`)}, nil)
	require.NoError(t, err)
	return task, duplicate
}

func TestTelegramAvailabilitySuspendsAllEffectsAndResumesFreshIntake(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	task, _ := availabilityIntake(t, store, 1)
	_, err := store.BindSubject(t.Context(), task.UserID, "person", "org")
	require.NoError(t, err)
	var calls atomic.Int32
	service := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden: bot was blocked by the user"}`)
	})
	client := &availableTelegram{Client: service.tg.(*telegram.Client), store: store, botID: 1}
	_, err = client.SendMessage(t.Context(), telegram.SendMessageRequest{ChatID: 42, Text: "test"})
	require.Error(t, err)
	state, err := store.TelegramAvailability(t.Context(), 1, 42)
	require.NoError(t, err)
	require.Equal(t, "bot_blocked", state.Reason)
	require.Error(t, client.SendActivity(t.Context(), 42, false))
	require.Error(t, client.SetMessageReaction(t.Context(), 42, 1, "👍"))
	require.Error(t, client.SetChatCommands(t.Context(), 42, "", nil))
	require.Error(t, client.SetChatMenuCommands(t.Context(), 42))
	_, err = client.SendDocument(
		t.Context(),
		telegram.SendDocumentRequest{ChatID: 42, Filename: "test.jpg", Content: []byte{1}},
	)
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load(), "paused effects never reach Telegram")
	recipients, err := store.MenuRecipients(t.Context(), 1, "org", uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, recipients)
	chats, err := store.TypingChats(t.Context(), 1, "org", 0)
	require.NoError(t, err)
	require.Empty(t, chats)
	require.NoError(t, store.CheckTelegramAvailability(t.Context(), 2, 42), "another bot is independent")
	worker := &Service{store: store, options: Options{AttemptTimeout: 5 * time.Second}}
	var snooze *river.JobSnoozeError
	require.ErrorAs(t, worker.work(t.Context(), task.ID), &snooze, "no IAM/model work starts")
	require.Equal(t, telegramAvailabilityPoll, snooze.Duration)
	user, err := store.GetUser(t.Context(), task.UserID)
	require.NoError(t, err)
	require.False(t, user.Revoked)
	require.Equal(t, "person", user.Subject)
	_, duplicate := availabilityIntake(t, store, 1)
	require.True(t, duplicate)
	require.ErrorIs(t, store.CheckTelegramAvailability(t.Context(), 1, 42), db.ErrTelegramUnavailable)
	// An acceptance rollback must not clear the pause.
	_, _, err = store.Intake(t.Context(), db.IntakeRequest{BotID: 1, UpdateID: 2, TelegramUserID: 42, ChatID: 42,
		Payload: json.RawMessage(`{}`)},
		func(context.Context, pgx.Tx, uuid.UUID) error { return errors.New("fixture rollback") })
	require.Error(t, err)
	require.ErrorIs(t, store.CheckTelegramAvailability(t.Context(), 1, 42), db.ErrTelegramUnavailable)
	_, duplicate = availabilityIntake(t, store, 2)
	require.False(t, duplicate)
	require.NoError(t, store.CheckTelegramAvailability(t.Context(), 1, 42))
	require.NoError(t, store.SuspendTelegram(t.Context(), 1, 42, state.Revision, "bot_blocked"))
	require.NoError(t, store.CheckTelegramAvailability(t.Context(), 1, 42), "stale failure cannot undo a fresh reply")
}

func TestTelegramAvailabilityRejectsOnlyConfirmedPrivate403(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		status      int
		description string
		reason      string
	}{
		{403, "Forbidden: user is deactivated", "user_deactivated"},
		{403, "Forbidden: bot was blocked by the user", "bot_blocked"},
		{403, "Forbidden: unrelated permission failure", ""},
		{400, "Bad Request: chat not found", ""},
		{429, "Too Many Requests", ""},
		{503, "Unavailable", ""},
	} {
		t.Run(test.description, func(t *testing.T) {
			t.Parallel()
			store := menuScanStore(t)
			service := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
				fmt.Fprintf(w, `{"ok":false,"error_code":%d,"description":%q}`, test.status, test.description)
			})
			client := &availableTelegram{Client: service.tg.(*telegram.Client), store: store, botID: 1}
			require.Error(t, client.SendActivity(t.Context(), 42, false))
			state, err := store.TelegramAvailability(t.Context(), 1, 42)
			require.NoError(t, err)
			require.Equal(t, test.reason, state.Reason)
		})
	}
}

func TestTelegramAvailabilityLateHTTPFailureCannotSuspendNewReply(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	availabilityIntake(t, store, 1)
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseRequest := func() { releaseOnce.Do(func() { close(release) }) }
	service := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		fmt.Fprint(w, `{"ok":false,"error_code":403,"description":"Forbidden: user is deactivated"}`)
	})
	t.Cleanup(releaseRequest)
	client := &availableTelegram{Client: service.tg.(*telegram.Client), store: store, botID: 1}
	done := make(chan error, 1)
	go func() { done <- client.SendActivity(t.Context(), 42, false) }()
	<-entered
	availabilityIntake(t, store, 2)
	releaseRequest()
	require.Error(t, <-done)
	require.NoError(t, store.CheckTelegramAvailability(t.Context(), 1, 42))
}

func TestTelegramAvailabilityKeepsStaticBanFeedbackIndependent(t *testing.T) {
	t.Parallel()
	store := menuScanStore(t)
	require.NoError(t, store.SuspendTelegram(t.Context(), 1, 42, 0, "bot_blocked"))
	var calls atomic.Int32
	service := menuHTTPService(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"ok":true,"result":{"message_id":1,"chat":{"id":42,"type":"private"}}}`)
	})
	service.tg = &availableTelegram{Client: service.tg.(*telegram.Client), store: store, botID: 1}
	service.replyToBannedUser(t.Context(), telegram.Update{ID: 1, Message: &telegram.Message{
		From: &telegram.User{ID: 42}, Chat: telegram.Chat{ID: 42, Type: "private"}}}, "")
	require.Equal(t, int32(1), calls.Load())
	require.ErrorIs(t, store.CheckTelegramAvailability(t.Context(), 1, 42), db.ErrTelegramUnavailable)
}
