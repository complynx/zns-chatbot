package integration_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredKnowledgeOpensFormWithoutLoomOrPrivateDetails(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	menu := h.submitText(t, 96600, "/menu")
	h.await(t, menu, "completed")
	h.awaitReplies(t, 1)
	id := h.submitText(t, 96601, "/knowledge")
	require.Equal(t, id, h.submitText(t, 96601, "/knowledge"))
	h.await(t, id, "completed")
	h.awaitReplies(t, 2)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submitText(t, 96601, "/knowledge"))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Nil(t, p.registrationDetails)
	require.Zero(t, p.loomUnavailableCalls)
	require.Empty(t, p.runs)
	require.Len(t, p.sent, 2)
	require.Contains(t, p.sent[0].Text, "/knowledge")
	menuKeys := p.sent[0].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Equal(t, appTestOrigin+"/app/#meals", menuKeys.InlineKeyboard[0][0].WebApp.URL)
	require.Contains(t, p.sent[1].Text, "does not publish or approve anything")
	form := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup)
	require.Len(t, form.InlineKeyboard, 1)
	button := form.InlineKeyboard[0][0]
	require.Equal(t, "Open knowledge base", button.Text)
	require.Equal(t, appTestOrigin+"/app/#knowledge", button.WebApp.URL)
	require.Empty(t, button.CallbackData)
}

func TestStructuredKnowledgeKeepsAgentQuestionAcrossOutageAndRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.miniAppOrigin = appTestOrigin
	p.question = true
	h := setup(t, p)
	original := h.submitText(t, 96700, "Ask me a question")
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
	entry := h.submitText(t, 96701, "/knowledge")
	h.await(t, entry, "completed")
	h.awaitReplies(t, 2)
	still, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	// Polling may wait for Loom; the saved question and its run must survive.
	require.Contains(t, []string{"awaiting_user", "awaiting_service"}, still.State)
	require.Equal(t, active.QuestionID, still.QuestionID)
	require.Equal(t, active.RunID, still.RunID)
	p.mu.Lock()
	require.Contains(t, p.sent[1].Text, "ничего не публикует")
	button := p.sent[1].ReplyMarkup.(*telegram.InlineKeyboardMarkup).InlineKeyboard[0][0]
	require.Equal(t, "Открыть базу знаний", button.Text)
	p.mu.Unlock()
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	answer := h.submitText(t, 96702, "Продолжай с моей статьёй")
	h.await(t, answer, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 3)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Equal(t, "Продолжай с моей статьёй", promptUserText(p.inputs[0].Text))
	require.Contains(t, p.inputs[0].Text, `"command":"/knowledge"`)
}

func TestStructuredKnowledgeUnavailableFormDoesNotStartAgent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	h := setup(t, p)
	id := h.submitText(t, 96800, "/knowledge")
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Contains(t, p.sent[0].Text, "currently unavailable")
	_, ok := p.sent[0].ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
	require.True(t, ok)
	require.Zero(t, p.loomUnavailableCalls)
	require.Empty(t, p.runs)
}
