package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredPaymentsNeedNoLoomAndReplayAfterRestart(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.loomDown = true
			p.miniAppOrigin = appTestOrigin
			h := setup(t, p)
			seedNamedTelegramUser(t, h)
			for n, command := range []string{"/start", "/payments"} {
				update := telegram.Update{ID: int64(98700 + n), Message: &telegram.Message{
					ID: int64(98700 + n), From: &telegram.User{ID: 456, LanguageCode: language},
					Chat: telegram.Chat{ID: 456, Type: "private"}, Text: command,
				}}
				raw, err := json.Marshal(update)
				require.NoError(t, err)
				accepted, _, err := h.service.Accept(t.Context(), update, raw)
				require.NoError(t, err)
				h.await(t, accepted.ID, "completed")
				h.awaitReplies(t, n+1)
				h.stop(t)
				h.start(t)
				replay, _, err := h.service.Accept(t.Context(), update, raw)
				require.NoError(t, err)
				require.Equal(t, accepted.ID, replay.ID)
			}
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Empty(t, p.runs)
			require.Empty(t, p.inputs)
			require.Zero(t, p.loomUnavailableCalls)
			require.Len(t, p.sent, 2)
			require.NotContains(t, p.sent[0].Text, "/payments")
			menu := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
			require.Equal(t, appTestOrigin+"/app/", menu.InlineKeyboard[0][0].WebApp.URL)
			form := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
			require.Equal(t, appTestOrigin+"/app/#pass-payments", form.InlineKeyboard[0][0].WebApp.URL)
			if language == "ru" {
				require.Contains(t, p.sent[0].Text, "ЗиНуСя")
				require.Equal(t, "Открыть оплату пасса", form.InlineKeyboard[0][0].Text)
				require.Contains(t, p.sent[1].Text, "оплату подтверждает")
			} else {
				require.Contains(t, p.sent[0].Text, "ZiNuSya")
				require.Equal(t, "Open pass payment", form.InlineKeyboard[0][0].Text)
				require.Contains(t, p.sent[1].Text, "only an ambassador or event administrator can confirm payment")
			}
		})
	}
}

func TestStructuredPaymentsPreservesAgentQuestionAndAppearsInReactiveContext(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	original := h.submitText(t, 98800, "Ask me a question")
	h.await(t, original, "awaiting_user")
	h.awaitReplies(t, 1)
	before, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	require.NotEmpty(t, before.QuestionID)
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	native := h.submitText(t, 98801, "/payments")
	h.await(t, native, "completed")
	h.awaitReplies(t, 2)
	active, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	require.Equal(t, before.RunID, active.RunID)
	require.Equal(t, before.QuestionID, active.QuestionID)
	p.mu.Lock()
	form := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Equal(t, appTestOrigin+"/app/#pass-payments", form.InlineKeyboard[0][0].WebApp.URL)
	require.Len(t, p.runs, 1)
	require.Empty(t, p.inputs)
	p.mu.Unlock()
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	answer := h.submitText(t, 98802, "Continue the original question")
	h.await(t, answer, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 3)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "Continue the original question", promptUserText(p.inputs[0].Text))
	require.Contains(t, p.inputs[0].Text, `"command":"/payments"`)
}
