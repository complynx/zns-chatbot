package integration_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestStructuredKnowledgeUsesAgentAndReplaysWithoutAnotherRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loomDown = true
	p.miniAppOrigin = appTestOrigin
	h := setup(t, p)
	menu := h.submitText(t, 96600, "/menu")
	h.await(t, menu, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	id := h.submitText(t, 96601, "/knowledge")
	require.Equal(t, id, h.submitText(t, 96601, "/knowledge"))
	h.await(t, id, "completed")
	h.awaitReplies(t, 3)
	h.stop(t)
	h.start(t)
	require.Equal(t, id, h.submitText(t, 96601, "/knowledge"))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Nil(t, p.registrationDetails)
	require.Len(t, p.runs, 1)
	require.Len(t, p.createdInputs, 1)
	require.Equal(t, "/knowledge", promptUserText(p.createdInputs[0]))
	require.Contains(t, p.createdInputs[0], `"command":"/menu"`)
	require.Len(t, p.sent, 3)
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[0].ReplyMarkup)
	require.NotContains(t, p.sent[1].Text, "/knowledge")
	require.IsType(t, &telegram.InlineKeyboardMarkup{}, p.sent[1].ReplyMarkup)
	require.Equal(t, "Hello, Daniel!", p.sent[2].Text)
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
	p.mu.Lock()
	p.loomDown = true
	p.mu.Unlock()
	entry := h.submitText(t, 96701, "/knowledge")
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return p.loomUnavailableCalls > 0
	}, 15*time.Second, 25*time.Millisecond)
	h.stop(t)
	still, err := h.store.GetTask(t.Context(), original)
	require.NoError(t, err)
	// Polling may wait for Loom; the saved question and its run must survive.
	require.Contains(t, []string{"awaiting_user", "awaiting_service"}, still.State)
	require.Equal(t, active.QuestionID, still.QuestionID)
	require.Equal(t, active.RunID, still.RunID)
	p.mu.Lock()
	require.Len(t, p.sent, 1)
	require.Len(t, p.runs, 1)
	require.Empty(t, p.inputs)
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, entry, "completed")
	h.await(t, original, "completed")
	h.awaitReplies(t, 2)
	require.Equal(t, entry, h.submitText(t, 96701, "/knowledge"))
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.runs, 1)
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Empty(t, p.inputs[0].QuestionID)
	require.Equal(t, "/knowledge", promptUserText(p.inputs[0].Text))
	require.Equal(t, "Thank you!", p.sent[1].Text)
}

func TestStructuredKnowledgeWithoutMiniAppStillUsesAgent(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	h := setup(t, p)
	id := h.submitText(t, 96800, "/knowledge")
	h.await(t, id, "completed")
	h.awaitReplies(t, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Equal(t, "Hello, Daniel!", p.sent[0].Text)
	require.Zero(t, p.loomUnavailableCalls)
	require.Len(t, p.runs, 1)
	require.Len(t, p.createdInputs, 1)
	require.Equal(t, "/knowledge", promptUserText(p.createdInputs[0]))
}
