package task

import (
	"context"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
	"github.com/complynx/zns-chatbot/internal/telegram"
)

const (
	runPaused             = "paused"
	runReconciling        = "reconciling"
	stepLimitReason       = "step_limit"
	budgetExhaustedReason = "budget_exhausted"
	stepLimitPromptPrefix = "step-limit:"
)

// allowancePaused reports a run that used its step or money allowance without a
// final result. The customer decides whether the work continues.
func allowancePaused(run loom.Run) bool {
	return run.Status == runPaused && (run.Reason == stepLimitReason || run.Reason == budgetExhaustedReason)
}

// budgetPaused reports a run that spent its own money allowance. Loom does not
// wake it: a message in the chat inbox replaces it with a successor run.
func budgetPaused(run loom.Run) bool {
	return run.Status == runPaused && run.Reason == budgetExhaustedReason
}

// Loom renews step_budget_start at each accepted input. The key identifies one exhausted budget.
func stepLimitPromptKey(run loom.Run) string {
	return stepLimitPromptPrefix + run.ID + ":budget:" + strconv.FormatInt(run.StepBudgetStart, 10)
}

// pendingStepLimitOutput returns the Continue/Stop notice once for each exhausted allowance.
// The checkpoint keeps the presented key, so polls and restarts do not repeat the notice.
func (s *Service) pendingStepLimitOutput(
	ctx context.Context, c *checkpoint, i intent, run loom.Run,
) []db.Outbound {
	prompt := stepLimitPromptKey(run)
	// A blocked run renews its budget at each input but stays in the same pause.
	blocked := run.PauseRequested || run.CancelRequested || len(run.UnresolvedSteps) > 0
	if c.KeyboardPrompt == prompt ||
		(blocked && strings.HasPrefix(c.KeyboardPrompt, stepLimitPromptPrefix+run.ID+":")) {
		return nil
	}
	c.KeyboardRevision++
	c.KeyboardPrompt = prompt
	ru := s.languageFor(ctx, i) == "ru"
	text := structuredText(ru,
		"I reached the step limit and the task is paused. Would you like to continue or stop? "+
			"Choose Continue or Stop below, or write your answer. "+
			"Some actions may already have completed; stopping will not undo them.",
		"Я достигла лимита шагов, и задача приостановлена. Продолжить работу или прекратить? "+
			"Выбери «Продолжить» или «Прекратить» ниже либо напиши ответ. "+
			"Часть действий могла уже выполниться; остановка их не отменит.",
	)
	if run.Reason == budgetExhaustedReason {
		text = structuredText(ru,
			"I used the work allowance of this task and it is paused. Would you like to continue or stop? "+
				"Choose Continue or Stop below, or write your answer. "+
				"Some actions may already have completed; stopping will not undo them.",
			"Я исчерпала лимит работы над этой задачей, и она приостановлена. Продолжить работу или прекратить? "+
				"Выбери «Продолжить» или «Прекратить» ниже либо напиши ответ. "+
				"Часть действий могла уже выполниться; остановка их не отменит.",
		)
	}
	if run.Result != "" {
		// Loom can save a progress note of the agent as the result of the paused run.
		text = run.Result + "\n\n" + text
	}
	out := s.result(ctx, i, text, prompt+":presentation:"+strconv.FormatInt(c.KeyboardRevision, 10))
	// Both buttons send ordinary conversation text. Loom wakes the run and the agent decides.
	setReplyMarkup(out, &telegram.ReplyKeyboardMarkup{
		Keyboard: [][]telegram.KeyboardButton{{
			{Text: structuredText(ru, "Continue", "Продолжить")},
			{Text: structuredText(ru, "Stop", "Прекратить")},
		}},
		ResizeKeyboard: true, OneTimeKeyboard: true,
	})
	return out
}

// stepLimitInputAck confirms an input that Loom accepted without waking the paused run.
// It returns nil when the run left the pause; the run then answers the user itself.
func (s *Service) stepLimitInputAck(ctx context.Context, i intent, receipt loom.ControlReceipt) []db.Outbound {
	if receipt.Status != runPaused || receipt.Reason != stepLimitReason {
		return nil
	}
	return s.result(ctx, i, structuredText(s.languageFor(ctx, i) == "ru",
		"Your message was sent to the task.", "Сообщение передано задаче."), "step-limit-input")
}
