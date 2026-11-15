package integration_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const stepLimitAck = "Your message was sent to the task."

func requireStepLimitNotice(t *testing.T, message telegram.SendMessageRequest) {
	t.Helper()
	require.Contains(t, message.Text, "step limit")
	require.Contains(t, message.Text, "continue or stop")
	require.IsType(t, &telegram.ReplyKeyboardMarkup{}, message.ReplyMarkup)
	keyboard := message.ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
	require.True(t, keyboard.OneTimeKeyboard)
	require.Equal(t, [][]telegram.KeyboardButton{{{Text: "Continue"}, {Text: "Stop"}}}, keyboard.Keyboard)
}

func TestStepLimitNotifiesOnceAndAcknowledgesBlockedInput(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 201)
	awaitLoomRun(t, h, id)
	h.stop(t)
	pauseMockRun(t, h, id, "step_limit")
	// A pause request keeps input from waking the run. Loom can also retain an unanswered prompt.
	p.mu.Lock()
	for key, run := range p.runs {
		run.PauseRequested = true
		run.Question = &loom.Question{ID: "old-question", Text: "Old prompt",
			Choices: []loom.Choice{{ID: "old", Label: "Old choice"}}, ExpiresAt: time.Now().Add(time.Hour)}
		p.runs[key] = run
	}
	p.mu.Unlock()
	h.start(t)
	h.awaitReplies(t, 1)
	h.await(t, id, "awaiting_service")
	h.stop(t)
	h.start(t)
	for n, text := range []string{"Continue", "Stop"} {
		followup := h.submitText(t, int64(202+n), text)
		h.await(t, followup, "completed")
		h.awaitReplies(t, n+2)
	}
	// Each input renewed the budget. The parent must observe that without a second notice.
	require.Eventually(t, func() bool {
		task, err := h.store.GetTask(t.Context(), id)
		var snapshot struct {
			Run loom.Run `json:"run"`
		}
		return err == nil && json.Unmarshal(task.Snapshot, &snapshot) == nil && snapshot.Run.StepBudgetStart == 2
	}, 5*time.Second, 20*time.Millisecond)
	completeMockRun(p)
	h.await(t, id, "completed")
	h.awaitReplies(t, 4)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 4, "Polls, a restart and blocked inputs must not repeat the notice")
	requireStepLimitNotice(t, p.sent[0])
	require.NotContains(t, p.sent[0].Text, "Old prompt")
	for _, message := range p.sent[1:3] {
		require.Equal(t, stepLimitAck, message.Text)
		require.Nil(t, message.ReplyMarkup)
	}
	require.Equal(t, "Recovered original run", p.sent[3].Text)
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[3].ReplyMarkup)
	require.Len(t, p.inputs, 2)
	require.Len(t, p.runs, 1)
	require.Empty(t, p.resumeKeys)
}

func TestStepLimitNotifiesForEachExhaustedBudgetUntilInputWakesRun(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 211)
	awaitLoomRun(t, h, id)
	pauseMockRun(t, h, id, "step_limit")
	h.awaitReplies(t, 1)
	// A renewed budget can run out while ZiNuSya is down. The running state is never observed.
	h.stop(t)
	p.mu.Lock()
	for key, run := range p.runs {
		run.StepBudgetStart++
		p.runs[key] = run
	}
	p.mu.Unlock()
	h.start(t)
	h.awaitReplies(t, 2)
	followup := h.submitText(t, 212, "Continue")
	h.await(t, followup, "completed")
	h.awaitReplies(t, 3)
	completeMockRun(p)
	h.await(t, id, "completed")
	h.awaitReplies(t, 4)
	h.stop(t)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.sent, 4, "An input that wakes the run needs no acknowledgement")
	requireStepLimitNotice(t, p.sent[0])
	requireStepLimitNotice(t, p.sent[1])
	require.Equal(t, "The task state has changed.", p.sent[2].Text)
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, p.sent[2].ReplyMarkup)
	require.Equal(t, "Recovered original run", p.sent[3].Text)
	require.Equal(t, "Continue", promptUserText(p.inputs[0].Text))
	require.Len(t, p.runs, 1)
	require.Empty(t, p.resumeKeys)
}
