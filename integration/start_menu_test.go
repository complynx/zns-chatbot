package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
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

func TestStartOnboardsBeforeWelcomeAndMenuAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, initial := range []string{"/start", "👋"} {
		t.Run(initial, func(t *testing.T) {
			t.Parallel()
			firstOnboardingBeforeWelcome(t, initial)
		})
	}
}

func firstOnboardingBeforeWelcome(t *testing.T, initial string) {
	t.Helper()
	p := newPeers(t)
	p.question = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	update := telegram.Update{
		ID: 98110,
		Message: &telegram.Message{
			ID:   98110,
			From: &telegram.User{ID: 456, LanguageCode: "pt-BR", FirstName: "Synthetic", LastName: "Hint"},
			Chat: telegram.Chat{ID: 456, Type: "private"},
			Text: initial,
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "awaiting_user")
	deliveredNamePrompt(t, h, accepted.ID)
	p.mu.Lock()
	require.Empty(t, p.menuCalls, "Menu must wait for confirmed name")
	require.Len(t, p.sent, 1, "Only the name question is delivered before onboarding")
	require.Contains(t, p.createdInputs[0], `"telegram_language_code":"pt-BR"`)
	require.Contains(t, p.createdInputs[0], `"needs_real_name":true`)
	p.mu.Unlock()
	scope, pending, token := agentRequestsName(t, h, accepted.ID)
	h.stop(t)
	h.start(t)
	code, result := profileRequest(
		t,
		h,
		token,
		"complete_name",
		uuid.New(),
		map[string]any{
			"expected_version": pending.Version,
			"expectation_id":   pending.NameExpectation.ID,
			"first_name":       "João",
			"last_name":        "da Silva",
			"first_name_latin": "João",
			"last_name_latin":  "da Silva",
		},
	)
	require.Equal(t, 200, code, string(result))
	answer := h.submitText(t, 98111, "João da Silva")
	h.await(t, answer, "completed")
	h.await(t, accepted.ID, "completed")
	deliveredNamePrompt(t, h, accepted.ID)
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return len(p.sent) == 3 },
		5*time.Second,
		10*time.Millisecond,
	)
	profile, err := h.store.GetProfile(t.Context(), scope)
	require.NoError(t, err)
	require.Equal(t, "João da Silva", profile.FullName)
	p.mu.Lock()
	require.Len(t, p.menuCalls, 3)
	require.Empty(t, p.menuCalls[0].Language)
	require.Equal(t, "ru", p.menuCalls[1].Language)
	require.Equal(t, int64(456), p.menuCalls[2].ChatID)
	require.Equal(t, "commands", p.menuCalls[2].Menu.Type)
	require.Contains(t, p.sent[2].Text, "Zinusya")
	require.Contains(t, p.sent[2].Text, "/menu")
	launch := p.sent[2].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Equal(t, appTestOrigin+"/app/", launch.InlineKeyboard[0][0].WebApp.URL)
	p.mu.Unlock()
	h.stop(t)
	h.start(t)
	replay, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	require.Equal(t, accepted.ID, replay.ID)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 3)
	require.Len(t, p.menuCalls, 3)
}

func TestStartCompletionWithoutConfirmedNameDoesNotSendWelcome(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submitText(t, 98120, "/start")
	h.await(t, id, "completed")
	deliveredNamePrompt(t, h, id)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.menuCalls)
	require.Len(t, p.sent, 1, "A model completion is not proof of name onboarding")
}
