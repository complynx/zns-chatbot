package integration_test

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func TestConversationReplyCarriesDeliveredHistoricalQuestionAfterReplacement(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "old", Label: "First"}}
	h := setup(t, p)
	id := h.submit(t, 96001)
	h.await(t, id, "awaiting_user")
	replyChoice(t, p, h, id)
	rows, err := h.store.ListOutbox(t.Context(), id)
	require.NoError(t, err)
	var receipt struct {
		MessageID int64 `json:"message_id"`
	}
	require.NoError(t, json.Unmarshal(rows[0].Receipt, &receipt))
	require.Positive(t, receipt.MessageID)
	p.mu.Lock()
	for key, run := range p.runs {
		q := *run.Question
		q.ID = "replacement-question"
		q.Text = "A different action?"
		q.Choices = []loom.Choice{{ID: "new", Label: "Different"}}
		run.Question = &q
		p.runs[key] = run
	}
	p.mu.Unlock()
	h.stop(t)
	h.start(t)
	update := telegram.Update{
		ID: 96002,
		Message: &telegram.Message{
			ID:             96002,
			From:           &telegram.User{ID: 456},
			Chat:           telegram.Chat{ID: 456, Type: "private"},
			Text:           "Yes",
			ReplyToMessage: &telegram.Message{ID: receipt.MessageID},
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 1)
	input := p.inputs[0]
	require.Equal(t, "steering", input.Kind)
	require.Empty(t, input.Decision)
	require.NotNil(t, input.Presentation)
	require.Equal(t, "question-1", input.Presentation.QuestionID)
	require.NotEmpty(t, input.Presentation.ReplyTo)
	for _, message := range p.sent {
		require.NotContains(t, message.Text, "_loom_context")
	}
}

func TestConversationProtocolCallbackRemainsBoundWhileQuestionUsesKeyboard(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "details", Label: "What will change?"}}
	p.approval = &loom.Approval{
		ID:           "critical-operation",
		ReplyMode:    "protocol",
		Text:         "Confirm operation",
		ArgumentHash: "binding",
		Arguments:    json.RawMessage(`{"amount":1.00,"target":"synthetic"}`),
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	h := setup(t, p)
	id := h.submit(t, 96011)
	h.await(t, id, "awaiting_user")
	label := replyChoice(t, p, h, id)
	require.Equal(t, "What will change?", label)
	p.mu.Lock()
	callback := ""
	for _, message := range p.sent {
		if markup, ok := message.ReplyMarkup.(*telegram.InlineKeyboardMarkup); ok {
			callback = markup.InlineKeyboard[0][0].CallbackData
		}
	}
	p.mu.Unlock()
	require.NotEmpty(t, callback)
	h.stop(t)
	h.start(t)
	update := telegram.Update{
		ID: 96012,
		CallbackQuery: &telegram.CallbackQuery{
			ID:      "critical-fixture",
			From:    telegram.User{ID: 456},
			Message: &telegram.Message{ID: 1, Chat: telegram.Chat{ID: 456, Type: "private"}},
			Data:    callback,
		},
	}
	raw, err := json.Marshal(update)
	require.NoError(t, err)
	accepted, _, err := h.service.Accept(t.Context(), update, raw)
	require.NoError(t, err)
	h.await(t, accepted.ID, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 1)
	require.Equal(t, "approval", p.inputs[0].Kind)
	require.Equal(t, "critical-operation", p.inputs[0].ApprovalID)
	require.Equal(t, "approve", p.inputs[0].Decision)
}

func TestConversationApprovalTextDoesNotBecomeBinaryClientDecision(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"Approve", "No", "Cancel", "Yes, but only for one person", "How much?"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.approval = &loom.Approval{
				ID:           "ordinary-action",
				ReplyMode:    loom.ReplyModeAgent,
				Text:         "Confirm action",
				ArgumentHash: "bound",
				ExpiresAt:    time.Now().Add(time.Hour),
			}
			h := setup(t, p)
			id := h.submit(t, 96021)
			h.await(t, id, "awaiting_user")
			require.Equal(t, "Approve", replyChoice(t, p, h, id))
			next := h.submitText(t, 96022, text)
			h.await(t, next, "completed")
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Len(t, p.inputs, 1)
			require.Equal(t, "steering", p.inputs[0].Kind)
			require.Equal(t, text, promptUserText(p.inputs[0].Text))
			require.Empty(t, p.inputs[0].Decision)
			require.Empty(t, p.inputs[0].ApprovalID)
			require.Empty(t, p.cancelKeys)
		})
	}
}

func TestConversationRestoresApprovalKeyboardAfterClarificationCompletes(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question, p.steering = true, true
	p.questionChoices = []loom.Choice{{ID: "clarify", Label: "First"}}
	p.approval = &loom.Approval{
		ID:           "still-pending",
		ReplyMode:    loom.ReplyModeAgent,
		Text:         "Confirm unchanged operation",
		ArgumentHash: "same-binding",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	h := setup(t, p)
	id := h.submit(t, 96031)
	h.await(t, id, "awaiting_user")
	require.Equal(t, "First", replyChoice(t, p, h, id))
	p.mu.Lock()
	for key, run := range p.runs {
		q := *run.Question
		q.Answer = &loom.InputRequest{Kind: "steering", Text: "Clarified"}
		run.Question = &q
		run.Status = "waiting_approval"
		run.Version++
		p.runs[key] = run
	}
	p.mu.Unlock()
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, message := range slices.Backward(p.sent) {
			if keyboard, ok := message.ReplyMarkup.(*telegram.ReplyKeyboardMarkup); ok {
				return keyboard.Keyboard[0][0].Text == "Approve"
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond, "Same blocked approval must receive a fresh keyboard after clarification")
	// Retire and restore that same action again, then remove it. Cleanup also
	// needs a fresh identity after the previous removal has already succeeded.
	updateRun := func(question *loom.Question, keepApproval bool) {
		p.mu.Lock()
		defer p.mu.Unlock()
		for key, run := range p.runs {
			run.Question = question
			if !keepApproval {
				run.Approval = nil
			}
			run.Version++
			p.runs[key] = run
		}
	}
	latestMarkup := func(removed bool) bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.sent) == 0 {
			return false
		}
		markup := p.sent[len(p.sent)-1].ReplyMarkup
		if removed {
			removal, ok := markup.(*telegram.ReplyKeyboardRemove)
			return ok && removal.RemoveKeyboard
		}
		keyboard, ok := markup.(*telegram.ReplyKeyboardMarkup)
		return ok && keyboard.Keyboard[0][0].Text == "Approve"
	}
	freeQuestion := &loom.Question{
		ID:        "free-clarification",
		Text:      "Clarify",
		AllowText: true,
		ExpiresAt: time.Now().Add(time.Hour),
	}
	updateRun(freeQuestion, true)
	require.Eventually(t, func() bool { return latestMarkup(true) }, 5*time.Second, 10*time.Millisecond)
	answered := *freeQuestion
	answered.Answer = &loom.InputRequest{Kind: "steering", Text: "Clarified"}
	updateRun(&answered, true)
	require.Eventually(t, func() bool { return latestMarkup(false) }, 5*time.Second, 10*time.Millisecond)
	updateRun(nil, false)
	require.Eventually(
		t,
		func() bool { return latestMarkup(true) },
		5*time.Second,
		10*time.Millisecond,
		"Repeated retirement must remove the restored keyboard",
	)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Empty(t, p.inputs)
	require.Len(t, p.createKeys, 1)
}

func TestConversationReservedCommandCaptionUsesNativePathThenAgentContinues(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "native", Label: "/profile"}}
	h := setup(t, p)
	id := h.submit(t, 96041)
	h.await(t, id, "awaiting_user")
	require.Equal(t, "/profile", replyChoice(t, p, h, id))
	native := h.submitText(t, 96042, "/profile")
	h.await(t, native, "completed")
	p.mu.Lock()
	require.Empty(t, p.inputs)
	require.Len(t, p.createKeys, 1)
	p.mu.Unlock()
	following := h.submitText(t, 96043, "Continue with my question")
	h.await(t, following, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Contains(t, p.inputs[0].Text, "Recent manual navigation requests")
	require.Contains(t, p.inputs[0].Text, "/profile")
	require.Len(t, p.createKeys, 1)
}
