package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredOrdersOpensFormWithoutLoomOrPrivateDetails(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	menu := h.submitText(t, 96200, "/menu")
	h.await(t, menu, "completed")
	h.awaitReplies(t, 2)
	id := h.submitText(t, 96201, "/orders")
	require.Equal(t, id, h.submitText(t, 96201, "/orders"))
	h.await(t, id, "completed")
	h.awaitReplies(t, 3)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submitText(t, 96201, "/orders"))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Nil(t, p.registrationDetails)
	require.Zero(t, p.loomUnavailableCalls)
	require.Empty(t, p.runs)
	require.Len(t, p.sent, 3)
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[0].ReplyMarkup)
	require.NotContains(t, p.sent[1].Text, "/orders")
	menuKeys := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Equal(t, appTestOrigin+"/app/", menuKeys.InlineKeyboard[0][0].WebApp.URL)
	require.Contains(t, p.sent[2].Text, "does not place an order")
	form := p.sent[2].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Len(t, form.InlineKeyboard, 1)
	button := form.InlineKeyboard[0][0]
	require.Equal(t, "Open saved selection", button.Text)
	require.Equal(t, appTestOrigin+"/app/#orders", button.WebApp.URL)
	require.Empty(t, button.CallbackData)
}

func TestStructuredOrdersKeepsAgentQuestionAcrossOutageAndRestart(t *testing.T) {
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
	require.NotEmpty(t, active.QuestionID)
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
	entry := h.submitText(t, 96301, "/orders")
	h.await(t, entry, "completed")
	h.awaitReplies(t, 2)
	still, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	// Polling may wait for Loom; the saved question and its run must survive.
	require.Contains(t, []string{"awaiting_user", "awaiting_service"}, still.State)
	require.Equal(t, active.QuestionID, still.QuestionID)
	require.Equal(t, active.RunID, still.RunID)
	p.mu.Lock()
	require.Contains(t, p.sent[1].Text, "не оформляет заказ")
	button := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0]
	require.Equal(t, "Открыть сохранённый выбор", button.Text)
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
	require.Contains(t, p.inputs[0].Text, `"command":"/orders"`)
}

func TestStructuredOrdersUnavailableFormDoesNotStartAgent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	id := h.submitText(t, 96400, "/orders")
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[0].Text, "currently unavailable")
	_, ok := p.sent[0].ReplyMarkup.(*telegram.ReplyKeyboardRemove)
	require.True(t, ok)
	require.Zero(t, p.loomUnavailableCalls)
	require.Empty(t, p.runs)
}
