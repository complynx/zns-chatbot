package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

func replyChoice(t *testing.T, p *peers, h *harness, id uuid.UUID) string {
	t.Helper()
	require.Eventually(t, func() bool {
		rows, err := h.store.ListOutbox(t.Context(), id)
		if err != nil {
			return false
		}
		return slices.ContainsFunc(rows, deliveredReplyKeyboard)
	}, 5*time.Second, 10*time.Millisecond)
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, message := range slices.Backward(p.sent) {
		if keyboard, ok := message.ReplyMarkup.(*telegram.ReplyKeyboardMarkup); ok {
			return keyboard.Keyboard[0][0].Text
		}
	}
	t.Fatal("No delivered reply keyboard")
	return ""
}

func deliveredReplyKeyboard(row db.Delivery) bool {
	if row.State != "delivered" {
		return false
	}
	var receipt struct {
		KeyboardHash string `json:"reply_keyboard_hash"`
	}
	if json.Unmarshal(row.Receipt, &receipt) != nil || receipt.KeyboardHash == "" {
		return false
	}
	var payload telegram.SendMessageRequest
	if json.Unmarshal(row.Payload, &payload) != nil {
		return false
	}
	keyboard, ok := payload.ReplyMarkup.(*telegram.ReplyKeyboardMarkup)
	return ok && len(keyboard.Keyboard) > 0
}

func TestConversationKeyboardAndTypedTextHaveIdenticalSteeringSemantics(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"Approve", "No", "Cancel", "Yes, but only for one person",
		"How much will it cost?", "/cancel"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			p := newPeers(t)
			p.question = true
			p.questionChoices = []loom.Choice{{ID: "suggestion", Label: text}}
			h := setup(t, p)
			id := h.submit(t, 92001)
			h.await(t, id, "awaiting_user")
			label := replyChoice(t, p, h, id)
			require.Equal(t, text, label)
			h.stop(t)
			h.start(t)
			following := h.submitText(t, 92002, label)
			h.await(t, following, "completed")
			h.await(t, id, "completed")
			p.mu.Lock()
			defer p.mu.Unlock()
			require.Len(t, p.inputs, 1)
			require.Equal(t, "steering", p.inputs[0].Kind)
			require.Equal(t, text, promptUserText(p.inputs[0].Text))
			require.Empty(t, p.inputs[0].Decision)
			require.Empty(t, p.inputs[0].ChoiceIDs)
			require.Empty(t, p.cancelKeys)
			for _, message := range p.sent {
				require.NotContains(t, message.Text, "⟦z:")
			}
		})
	}
}

func TestReplyKeyboardEmptyCompletionStillRemovesChoices(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "one", Label: "First"}}
	p.inputResult = new(string)
	h := setup(t, p)
	id := h.submit(t, 92401)
	h.await(t, id, "awaiting_user")
	label := replyChoice(t, p, h, id)
	answer := h.submitText(t, 92402, label)
	h.await(t, answer, "completed")
	h.await(t, id, "completed")
	h.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	last := p.sent[len(p.sent)-1]
	require.Equal(t, "Done.", last.Text)
	require.IsType(t, &telegram.ReplyKeyboardRemove{}, last.ReplyMarkup)
}

func TestReplyKeyboardCleanupUsesDeliveryOrderAfterLaterMenu(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering = true
	h := setup(t, p)
	id := h.submit(t, 92501)
	h.await(t, id, "running")
	menu := h.submitText(t, 92502, "/menu")
	h.await(t, menu, "completed")
	h.awaitReplies(t, 1)
	require.Eventually(t, func() bool {
		record, err := h.store.GetTask(t.Context(), id)
		return err == nil && record.RunID != "" && record.State == "running"
	}, 15*time.Second, 25*time.Millisecond)
	p.mu.Lock()
	p.steering = false
	for key, run := range p.runs {
		run.Status = "awaiting_input"
		run.Question = &loom.Question{
			ID:        "later-question",
			Text:      "Choose",
			Choices:   []loom.Choice{{ID: "one", Label: "First"}},
			ExpiresAt: time.Now().Add(time.Hour),
		}
		p.runs[key] = run
	}
	p.mu.Unlock()
	label := replyChoice(t, p, h, id)
	answer := h.submitText(t, 92503, label)
	h.await(t, answer, "completed")
	h.await(t, id, "completed")
	h.awaitReplies(t, 3)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.IsType(
		t,
		&telegram.ReplyKeyboardRemove{},
		p.sent[len(p.sent)-1].ReplyMarkup,
		"Question delivered after the newer menu must still be removed",
	)
}

func TestReplyKeyboardLostInputReceiptReconcilesCompletedTargetAfterRestart(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "one", Label: "First"}}
	h := setup(t, p)
	id := h.submit(t, 92601)
	h.await(t, id, "awaiting_user")
	label := replyChoice(t, p, h, id)
	p.mu.Lock()
	p.loseInputAndStopLoom = true
	p.mu.Unlock()
	answer := h.submitText(t, 92602, label)
	require.Eventually(
		t,
		func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.loomDown && len(p.inputs) == 1 },
		5*time.Second,
		10*time.Millisecond,
	)
	h.stop(t)
	p.mu.Lock()
	p.loomDown = false
	p.mu.Unlock()
	h.start(t)
	h.await(t, answer, "completed")
	h.await(t, id, "completed")
	record, err := h.store.GetTask(t.Context(), answer)
	require.NoError(t, err)
	require.Equal(
		t,
		"run-"+id.String(),
		record.RunID,
		"Completion must reflect the reconciled input, not no-active fallback",
	)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 1)
	require.Len(t, p.createKeys, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Equal(t, label, promptUserText(p.inputs[0].Text))
}

func TestReplyKeyboardCleanupStillRunsAfterNewPlainTaskStarts(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "one", Label: "First"}}
	h := setup(t, p)
	id := h.submit(t, 92701)
	h.await(t, id, "awaiting_user")
	label := replyChoice(t, p, h, id)
	blocker, err := h.store.Pool().Acquire(t.Context())
	require.NoError(t, err)
	_, err = blocker.Exec(t.Context(), `SELECT pg_advisory_lock(hashtextextended($1,0))`, id.String())
	require.NoError(t, err)
	var blockerPID int32
	require.NoError(t, blocker.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&blockerPID))
	released := false
	t.Cleanup(func() {
		if !released {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, _ = blocker.Exec(ctx, `SELECT pg_advisory_unlock_all()`)
			blocker.Release()
		}
	})
	// Hold the terminal outbox reservation after Save clears the old active task.
	_, err = h.store.Pool().
		Exec(t.Context(), fmt.Sprintf("CREATE FUNCTION hold_keyboard_cleanup() RETURNS trigger LANGUAGE plpgsql "+
			"AS $$ BEGIN\n IF NEW.state='send_inflight' AND NEW.payload#>>'{reply_markup,remove_keyboard}'='true' "+
			"THEN\n PERFORM pg_advisory_lock(hashtextextended('%s',0));PERFORM "+
			"pg_advisory_unlock(hashtextextended('%s',0));END IF;\n RETURN NEW;END $$;CREATE TRIGGER "+
			"hold_keyboard_cleanup BEFORE UPDATE ON shuttle_outbox FOR EACH ROW EXECUTE FUNCTION "+
			"hold_keyboard_cleanup()", id, id))
	require.NoError(t, err)
	answer := h.submitText(t, 92702, label)
	h.await(t, answer, "completed")
	h.await(t, id, "completed")
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := h.store.Pool().
			QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))",
				blockerPID).
			Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	p.mu.Lock()
	p.question = false
	p.steering = true
	p.mu.Unlock()
	next := h.submitText(t, 92703, "Continue with an unrelated plain task")
	h.await(t, next, "running")
	_, err = blocker.Exec(t.Context(), `SELECT pg_advisory_unlock_all()`)
	require.NoError(t, err)
	blocker.Release()
	released = true
	h.awaitReplies(t, 2)
	p.mu.Lock()
	defer p.mu.Unlock()
	require.IsType(
		t,
		&telegram.ReplyKeyboardRemove{},
		p.sent[len(p.sent)-1].ReplyMarkup,
		"A new task without a keyboard must not retain the old choices",
	)
}
func TestResolvedQuestionRetiresKeyboardWhileAgentContinues(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.retainResolvedPrompts, p.question = true, true, true
	p.questionChoices = []loom.Choice{{ID: "one", Label: "First"}}
	h := setup(t, p)
	id := h.submit(t, 92801)
	h.await(t, id, "awaiting_user")
	label := replyChoice(t, p, h, id)
	answer := h.submitText(t, 92802, label)
	h.await(t, answer, "completed")
	h.await(t, id, "running")
	awaitReplyKeyboardRemoval(t, p)
	var acknowledgements []string
	p.mu.Lock()
	for _, message := range p.sent {
		if _, removed := message.ReplyMarkup.(*telegram.ReplyKeyboardRemove); removed {
			acknowledgements = append(acknowledgements, message.Text)
		}
	}
	p.mu.Unlock()
	require.NotEmpty(t, acknowledgements)
	for _, text := range acknowledgements {
		require.Equal(t, "Response received.", text)
	}
	following := h.submitText(t, 92803, label)
	h.await(t, following, "completed")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 2)
	require.Equal(t, "steering", p.inputs[1].Kind)
	require.Len(t, p.createKeys, 1)
}

func TestResolvedApprovalDoesNotHideNextQuestion(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.steering, p.retainResolvedPrompts, p.question = true, true, true
	p.questionChoices = []loom.Choice{{ID: "fresh", Label: "Fresh choice"}}
	p.approval = &loom.Approval{
		ID:        "old-approval",
		Decision:  "approve",
		Text:      "Already approved",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	h := setup(t, p)
	id := h.submit(t, 92901)
	h.await(t, id, "awaiting_user")
	label := replyChoice(t, p, h, id)
	require.Contains(t, label, "Fresh choice")
	answer := h.submitText(t, 92902, label)
	h.await(t, answer, "completed")
	h.await(t, id, "running")
	p.mu.Lock()
	defer p.mu.Unlock()
	require.Len(t, p.inputs, 1)
	require.Equal(t, "steering", p.inputs[0].Kind)
	require.Equal(t, label, promptUserText(p.inputs[0].Text))
}

func awaitReplyKeyboardRemoval(t *testing.T, p *peers) {
	t.Helper()
	require.Eventually(t, func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, message := range p.sent {
			if markup, ok := message.ReplyMarkup.(*telegram.ReplyKeyboardRemove); ok && markup.RemoveKeyboard {
				return true
			}
		}
		return false
	}, 5*time.Second, 10*time.Millisecond)
}

func TestExpiredQuestionDoesNotAcknowledgeAnOlderApprovedAction(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.question = true
	p.questionChoices = []loom.Choice{{ID: "fresh", Label: "Fresh choice"}}
	p.approval = &loom.Approval{
		ID: "old", Decision: "approve", Text: "Old action", ExpiresAt: time.Now().Add(time.Hour),
	}
	h := setup(t, p)
	id := h.submit(t, 92911)
	h.await(t, id, "awaiting_user")
	_ = replyChoice(t, p, h, id)
	task, err := h.store.GetTask(t.Context(), id)
	require.NoError(t, err)
	var expired bool
	p.mu.Lock()
	for key, run := range p.runs {
		if run.ID == task.RunID && run.Question != nil {
			question := *run.Question
			question.ExpiresAt = time.Now().Add(-time.Minute)
			run.Question = &question
			p.runs[key] = run
			expired = true
		}
	}
	p.mu.Unlock()
	require.True(t, expired)
	awaitReplyKeyboardRemoval(t, p)
	var warnings []string
	p.mu.Lock()
	for _, message := range p.sent {
		if _, removed := message.ReplyMarkup.(*telegram.ReplyKeyboardRemove); removed {
			warnings = append(warnings, message.Text)
		}
	}
	p.mu.Unlock()
	require.NotEmpty(t, warnings)
	for _, text := range warnings {
		require.Equal(t, "This choice is no longer active.", text)
	}
}
