package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

const (
	// chatRetryWindow limits the wait for Loom to clear a blocker of the chat.
	chatRetryWindow             = 5 * time.Minute
	reasonAuthorizationRequired = "authorization_required"
	reasonCreditsExhausted      = "credits_exhausted"
)

// retainedMessageError reports an inbox message that Loom keeps without delivery.
type retainedMessageError struct{ receipt loom.MessageReceipt }

func (*retainedMessageError) Error() string { return "Loom retained the message without delivery" }

// conflictWith reports a conflict that Loom itself returned. An empty code accepts each code.
func conflictWith(err error, code string) bool {
	upstream, ok := errors.AsType[*loom.Error](err)
	return ok && upstream.Outcome == loom.Rejected && upstream.StatusCode == http.StatusConflict &&
		(code == "" || upstream.Code == code)
}

// superseded reports a run that Loom replaced with a successor run in the same chat.
func superseded(run loom.Run) bool {
	return run.Status == stateCancelled && run.Reason == "superseded"
}

// receiptsUnavailable reports a successor run that Loom cannot give the operation
// receipts of the replaced run. No input continues such a run.
func receiptsUnavailable(run loom.Run) bool {
	return run.Status == runPaused &&
		(run.Reason == "recovery_context_too_large" || run.Reason == "recovery_context_unavailable")
}

// authorizationLost reports a waiting run that a session renewal does not continue.
// Loom gives a successor run the second reason when the current authority cannot
// read the operation receipts of the replaced run.
func authorizationLost(run loom.Run) bool {
	return run.Reason == "permission_revoked" || run.Reason == "recovery_authorization_required"
}

// chatRetryExpired starts the retry window at the first call and reports its end.
// The caller saves the checkpoint.
func chatRetryExpired(c *checkpoint) bool {
	if c.ChatRetryUntil == nil {
		until := time.Now().Add(chatRetryWindow)
		c.ChatRetryUntil = &until
	}
	return !time.Now().Before(*c.ChatRetryUntil)
}

// dispatchControl sends the saved control through its saved route. The route is
// part of the checkpoint: the two Loom endpoints do not share idempotency keys,
// so a retry through the other endpoint could apply the same text twice.
func (s *Service) dispatchControl(
	ctx context.Context, auth loom.Auth, t db.Task, c checkpoint, runID string,
) (loom.ControlReceipt, error) {
	if c.InboxChatID == "" {
		return s.loom.Input(ctx, auth, runID, t.ID.String(), *c.Control)
	}
	receipt, err := s.loom.SendMessage(ctx, auth, c.InboxChatID, t.ID.String(), c.Control.Text)
	if err != nil {
		return loom.ControlReceipt{}, err
	}
	if !receipt.Delivered {
		return loom.ControlReceipt{}, &retainedMessageError{receipt}
	}
	return loom.ControlReceipt{
		ID: receipt.RunID, InputID: receipt.InputID, Status: receipt.Status, Reason: receipt.Reason,
	}, nil
}

// withdrawRetainedMessage handles a message that Loom keeps without delivery.
// Such a message blocks all later messages of the chat. The result is true when
// the message is withdrawn: Loom did not apply it and never does. Otherwise the
// error is the wait before the next attempt. With patient set, the task waits a
// limited time for a blocker that clears without the customer: Loom stops the
// replaced run, or the observer of the run renews its session.
// c.InboxBlocker keeps the reason of the withdrawal: after a lost response Loom
// reports the message only as abandoned.
func (s *Service) withdrawRetainedMessage(
	ctx context.Context, t *db.Task, c *checkpoint, i intent, auth loom.Auth, m loom.MessageReceipt, patient bool,
) (bool, error) {
	if m.Abandoned {
		return true, nil
	}
	expired := chatRetryExpired(c)
	transient := m.Reason == "cancellation_pending" || m.Reason == reasonAuthorizationRequired
	withdraw := m.MessageID
	if m.Reason == "earlier_message_pending" && m.BlockingMessageID != "" {
		// Controls of one run are serial, so no active task owns the earlier message.
		withdraw = m.BlockingMessageID
	}
	if transient && patient && !expired {
		return false, s.waitForService(ctx, t, *c)
	}
	if withdraw == m.MessageID && c.InboxBlocker != m.Reason {
		c.InboxBlocker = m.Reason
		if err := s.save(ctx, t, *c, nil); err != nil {
			return false, s.wait()
		}
	}
	err := s.loom.AbandonMessage(ctx, auth, c.InboxChatID, withdraw, t.ID.String()+":abandon:"+withdraw)
	if err == nil && withdraw == m.MessageID {
		return true, nil
	}
	// A conflict means that Loom delivered the message; the next attempt reads its receipt.
	if err != nil && !conflictWith(err, "") {
		return false, s.loomError(ctx, t, *c, i, err)
	}
	return false, s.waitForService(ctx, t, *c)
}

// waitForService saves the checkpoint and waits for the next attempt.
func (s *Service) waitForService(ctx context.Context, t *db.Task, c checkpoint) error {
	t.State = stateAwaitingService
	_ = s.save(ctx, t, c, nil)
	return s.wait()
}

// retainedAfterTarget handles a retained message of a control whose target task
// ended: no observer clears its blocker. The result has the form of
// reconcileFinishedInput: handled is false when the withdrawn text can become its
// own run. A withdrawn message is proof that Loom did not apply the text.
func (s *Service) retainedAfterTarget(
	ctx context.Context, t *db.Task, c checkpoint, i intent, auth loom.Auth, m loom.MessageReceipt,
) (bool, error) {
	withdrawn, wait := s.withdrawRetainedMessage(ctx, t, &c, i, auth, m, false)
	if withdrawn && c.InboxBlocker == loom.ReasonEffectUncertain {
		// Loom refused this text because of an unknown effect: do not start new work with it.
		return true, s.finish(ctx, t, c, i, stateFailed, stateFailed)
	}
	return !withdrawn, wait
}

// retainedMessageNotice selects the customer notice for a withdrawn message.
func retainedMessageNotice(blocker string) string {
	if blocker == reasonCreditsExhausted {
		return reasonCreditsExhausted
	}
	return stateFailed
}

// followSuccessor moves the observer of a replaced run to the current run of the
// chat. An inbox message that continues a spent run makes Loom cancel that run as
// superseded and admit a successor with the same authorization session.
func (s *Service) followSuccessor(
	ctx context.Context, t *db.Task, c checkpoint, i intent, auth loom.Auth, run loom.Run,
) error {
	current, err := s.loom.CurrentRun(ctx, auth, run.ChatID)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	if current.RunID != "" && current.RunID != run.ID {
		t.RunID = current.RunID
	}
	return s.waitForService(ctx, t, c)
}

// adoptRun makes this control task the observer of the run that its inbox
// message reached. Call it when the target task is terminal and its run is not
// that run: the target run ended for another cause, and Loom started a new round.
// Loom gives that run the authorization session of the target run, so this task
// continues with the session receipt of the target.
func (s *Service) adoptRun(
	ctx context.Context, t *db.Task, i intent, user db.User, service string, target db.Task, runID string,
) error {
	var saved checkpoint
	if err := json.Unmarshal(target.Snapshot, &saved); err != nil {
		return s.finish(ctx, t, checkpoint{}, i, stateFailed, stateFailed)
	}
	c := checkpoint{Session: saved.Session}
	snapshot, err := json.Marshal(c)
	if err != nil {
		return err
	}
	adopted, err := s.store.AdoptRun(ctx, t.ID, s.owner, t.Fence,
		db.AdoptedRun{RunID: runID, AuthSessionID: target.AuthSessionID, Snapshot: snapshot})
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "run_adopt", err)
		return s.wait()
	}
	*t = adopted
	return s.runTask(ctx, t, c, i, user, service)
}

// cancelUnobservedRun cancels a run that no local task observes. It does not
// cancel a run that Loom reconciles, and then reports true.
func (s *Service) cancelUnobservedRun(ctx context.Context, auth loom.Auth, runID, key string) (bool, error) {
	run, err := s.loom.GetRun(ctx, auth, runID)
	if err != nil || run.Status == runReconciling {
		return err == nil, err
	}
	_, err = s.loom.Cancel(ctx, auth, runID, key)
	return false, err
}

// releaseChat removes what blocks the creation of a run in the default chat.
// Call it only for a creation conflict of a task that the conversation routed
// as a new run. No local task observes the blocking run or message then: its
// observer failed or was closed. The run cannot deliver a result, so cancel it;
// the new run reads the same chat history. The next attempt retries the creation.
// A run with an unknown effect stays for an operator, and the request fails.
// The locator can report another blocker in front of that state, so read the run.
func (s *Service) releaseChat(
	ctx context.Context, t *db.Task, c checkpoint, i intent, auth loom.Auth, conflict error,
) error {
	if chatRetryExpired(&c) {
		return s.loomError(ctx, t, c, i, conflict)
	}
	chat, err := s.loom.DefaultChat(ctx, auth)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	current, err := s.loom.CurrentRun(ctx, auth, chat)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	key := t.ID.String() + ":release:"
	if current.BlockingMessageID != "" {
		// The locator reports a retained message before the state of the run.
		err = s.loom.AbandonMessage(ctx, auth, chat, current.BlockingMessageID, key+current.BlockingMessageID)
		if err == nil {
			current, err = s.loom.CurrentRun(ctx, auth, chat)
		}
	}
	uncertain := err == nil && current.Reason == loom.ReasonEffectUncertain
	if err == nil && !uncertain && current.BlockingMessageID == "" && current.RunID != "" &&
		current.Status != "terminal" {
		uncertain, err = s.cancelUnobservedRun(ctx, auth, current.RunID, key+current.RunID)
	}
	if uncertain {
		return s.loomError(ctx, t, c, i, conflict)
	}
	// A conflict means that the run or message finished without this request.
	if err != nil && !conflictWith(err, "") {
		return s.loomError(ctx, t, c, i, err)
	}
	return s.waitForService(ctx, t, c)
}
