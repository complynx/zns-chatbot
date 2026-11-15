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
	stepLimitReason       = "step_limit"
	stepLimitPromptPrefix = "step-limit:"
)

func stepLimitPaused(run loom.Run) bool {
	return run.Status == runPaused && run.Reason == stepLimitReason
}

// Loom renews step_budget_start at each accepted input. The key identifies one exhausted budget.
func stepLimitPromptKey(run loom.Run) string {
	return stepLimitPromptPrefix + run.ID + ":budget:" + strconv.FormatInt(run.StepBudgetStart, 10)
}

// pendingStepLimitOutput returns the Continue/Stop notice once for each exhausted budget.
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
	out := s.result(ctx, i, structuredText(ru,
		"I reached the step limit and the task is paused. Would you like to continue or stop? "+
			"Choose Continue or Stop below, or write your answer. "+
			"Some actions may already have completed; stopping will not undo them.",
		"Я достигла лимита шагов, и задача приостановлена. Продолжить работу или прекратить? "+
			"Выбери «Продолжить» или «Прекратить» ниже либо напиши ответ. "+
			"Часть действий могла уже выполниться; остановка их не отменит.",
	), prompt+":presentation:"+strconv.FormatInt(c.KeyboardRevision, 10))
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
