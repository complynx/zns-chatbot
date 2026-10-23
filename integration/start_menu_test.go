package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

type menuCall struct {
	Method   string                `json:"-"`
	Commands []telegram.BotCommand `json:"commands"`
	Scope    struct {
		Type   string `json:"type"`
		ChatID int64  `json:"chat_id"`
	} `json:"scope"`
	Language string `json:"language_code"`
	ChatID   int64  `json:"chat_id"`
	Menu     struct {
		Type string `json:"type"`
	} `json:"menu_button"`
}

func TestStartRefreshesMenuBeforeNameDialogueAndPreservesWelcomeAcrossRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	update := telegram.Update{ID: 98110, Message: &telegram.Message{
		ID: 98110, From: &telegram.User{ID: 456, LanguageCode: "ru", FirstName: "Synthetic", LastName: "Hint"},
		Chat: telegram.Chat{ID: 456, Type: "private"}, Text: "/start",
	}}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "awaiting_user")
	deliveredNamePrompt(t, h, accepted.ID)
	p.mu.Lock()
	require.Len(t, p.menuCalls, 3)
	for _, call := range p.menuCalls[:2] {
		require.Equal(t, "chat", call.Scope.Type)
		require.Equal(t, int64(456), call.Scope.ChatID)
		require.NotEmpty(t, call.Commands)
		for _, command := range call.Commands {
			require.NotEqual(t, "passes", command.Command)
			require.NotEqual(t, "payment_reviews", command.Command)
		}
	}
	require.Empty(t, p.menuCalls[0].Language)
	require.Equal(t, "ru", p.menuCalls[1].Language)
	require.Equal(t, int64(456), p.menuCalls[2].ChatID)
	require.Equal(t, "commands", p.menuCalls[2].Menu.Type)
	require.Len(t, p.sent, 2)
	require.Contains(t, p.sent[0].Text, "Я ЗиНуСя")
	require.Contains(t, p.sent[0].Text, "/menu")
	require.Contains(t, p.sent[0].Text, "имя и фамилию")
	launch := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Equal(t, appTestOrigin+"/app/", launch.InlineKeyboard[0][0].WebApp.URL)
	require.Len(t, p.createdInputs, 1)
	require.Contains(t, p.createdInputs[0], `"needs_real_name":true`)
	p.mu.Unlock()
	profile, err := h.store.GetProfile(t.Context(), ownerProfileScope(t, h, accepted.UserID))
	require.NoError(t, err)
	require.Empty(t, profile.FullName, "Native guidance does not adopt Telegram name hints")
	h.stop(t)
	h.start(t)
	replay, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	require.Equal(t, accepted.ID, replay.ID)
	deliveredNamePrompt(t, h, accepted.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 2)
	require.Len(t, p.menuCalls, 3)
}

func TestStartUnknownWelcomeDeliveryDoesNotStartNameAgent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question, p.loseSend = true, true
	h := setup(t, p)
	id := h.submitText(t, 98120, "/start")
	require.Eventually(t, func() bool {
		rows, err := h.store.ListOutbox(t.Context(), id)
		return err == nil && len(rows) == 1 && rows[0].State == "delivery_unknown"
	}, 5*time.Second, 5*time.Millisecond)
	h.await(t, id, "delivery_unknown")
	current, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	require.Empty(t, current.RunID)
	rows, err := h.store.ListOutbox(t.Context(), id)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "delivery_unknown", rows[0].State)
	needed, err := h.store.NeedsTyping(t.Context(), 123, "org", 456)
	require.NoError(t, err)
	require.False(t, needed, "An uncertain pre-run greeting is not processing")
	h.stop(t)
	h.start(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.runs)
	require.Empty(t, p.createdInputs)
	require.Len(t, p.menuCalls, 3, "The fenced task must not reapply chat settings")
}
