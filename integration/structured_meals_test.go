package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredMealsKeepsAgentQuestionAcrossOutageAndRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.question = true
	h := setup(t, p)
	original := h.submitText(t, 96300, "Ask me a question")
	h.await(t, original, "awaiting_user")
	h.awaitReplies(t, 1)
	active, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	user, err := h.store.GetUser(t.Context(), active.UserID)
	require.NoError(t, err)
	scope := db.ProfileScope{BotID: 123, Organization: user.Organization, Subject: user.Subject, UserID: user.ID}
	_, err = h.store.MutateProfile(
		t.Context(),
		scope,
		db.ProfileMutation{OperationID: uuid.New(), Kind: "set_language", BotLanguage: "ru"},
	)
	require.NoError(t, err)
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	entry := h.submitText(t, 96301, "/menu")
	h.await(t, entry, "completed")
	h.awaitReplies(t, 2)
	still, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	require.Equal(t, "awaiting_user", still.State)
	p.mu.Lock()
	require.Contains(t, p.sent[1].Text, "не оформляет заказ")
	button := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0]
	require.Equal(t, "Открыть питание", button.Text)
	require.Zero(t, p.loomUnavailableCalls)
	p.mu.Unlock()
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	answer := h.submitText(t, 96302, "Продолжай с моим сохранённым выбором")
	h.await(t, answer, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 3)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Equal(t, "Продолжай с моим сохранённым выбором", promptUserText(p.inputs[0].Text))
	require.Contains(t, p.inputs[0].Text, `"command":"/menu"`)
}
