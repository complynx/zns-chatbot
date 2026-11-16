package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

const recoveryRetryWindow = 2 * time.Minute
const routeNewRun = "new_run"
const reasonAuthorizationRequired = "authorization_required"
const runWaitingAuth = "waiting_auth"
const reasonShutdown = "shutdown"

type messageRecovery struct {
	ChatID       string                `json:"chat_id"`
	Request      loom.RecoverMessage   `json:"request"`
	Receipt      *loom.RecoveryReceipt `json:"receipt,omitempty"`
	RetryUntil   time.Time             `json:"retry_until"`
	WakeSequence int64                 `json:"wake_sequence"`
}

func (s *Service) startConversationRecovery(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	user db.User,
	service string,
) (bool, error) {
	ready, seq, err := s.store.RecoveryReady(ctx, *t)
	if err != nil || !ready {
		return true, s.wait()
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return true, s.identityError(ctx, t, *c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	supported, err := s.loom.SupportsConversationRecovery(ctx, auth)
	if err != nil {
		return true, s.recoveryCapabilityError(ctx, t, *c, i, err)
	}
	if !supported {
		return false, nil
	}
	chat, err := s.loom.DefaultChat(ctx, auth)
	if err != nil {
		return true, s.loomError(ctx, t, *c, i, err)
	}
	located, err := s.loom.ActiveRun(ctx, auth, chat)
	if err != nil {
		return true, s.loomError(ctx, t, *c, i, err)
	}
	if located.Status == "idle" && located.NextAction == "create_run" {
		return false, nil
	}
	text, err := s.recoveryMessageText(ctx, *t, *c, i)
	if err != nil {
		return true, s.wait()
	}
	c.Recovery = &messageRecovery{
		ChatID:       chat,
		Request:      loom.RecoverMessage{Text: text},
		RetryUntil:   time.Now().Add(recoveryRetryWindow),
		WakeSequence: seq,
	}
	// Only replace technical pauses; an explicit stop or unknown effect is never bypassed.
	if located.NextAction == "resolve_blocker_then_resume" && replaceablePause(located.Reason) ||
		located.NextAction == "select_allowed_model_or_restore_access" {
		c.Recovery.Request.ReplaceRunID = located.RunID
	}
	if err = s.save(ctx, t, *c, nil); err != nil {
		return true, s.wait()
	}
	return true, s.recoverConversation(ctx, t, *c, i, user, service)
}

func (s *Service) recoveryMessageText(ctx context.Context, t db.Task, c checkpoint, i intent) (string, error) {
	if c.Create != nil {
		return c.Create.Input.Text, nil
	}
	text, err := s.profilePrompt(ctx, t, i)
	return text + reactionInstructions, err
}

func (s *Service) recoverBusyCreation(ctx context.Context, t *db.Task, c *checkpoint, i intent,
	auth loom.Auth, failure error) (bool, error) {
	upstream, ok := errors.AsType[*loom.Error](failure)
	if !ok || upstream.Outcome != loom.Rejected || upstream.StatusCode != http.StatusConflict ||
		upstream.Code != "chat_busy" || t.RunID != "" || c.Create == nil || i.Kind != kindMessage {
		return false, nil
	}
	// Loom replays creation idempotency before chat_busy. This typed rejection
	// proves the saved creation did not admit a run; retain its exact text.
	user, err := s.store.GetUser(ctx, t.UserID)
	if err != nil {
		return true, s.wait()
	}
	return s.startConversationRecovery(ctx, t, c, i, user, auth.ServiceToken)
}

func (s *Service) recoveryCapabilityError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	upstream, ok := errors.AsType[*loom.Error](err)
	if !ok || !upstream.Retryable || t.AuthSessionID != "" {
		return s.loomError(ctx, t, c, i, err)
	}
	// Preserve the initial authorization stage while Loom is unavailable.
	t.State = stateAwaitingAuth
	return s.waitWithRecoveryOutput(ctx, t, c, nil)
}

func replaceablePause(reason string) bool {
	return reason == "context_unavailable" || reason == "queue_attempts_exhausted" || reason == reasonShutdown
}

func (s *Service) recoverConversation(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	user db.User,
	service string,
) error {
	r := c.Recovery
	if r.Receipt != nil && r.Receipt.Delivered && t.Route == routeNewRun && t.RunID != "" {
		return s.observeRecoveredRun(ctx, t, c, i, user, service)
	}
	ready, seq, err := s.store.RecoveryReady(ctx, *t)
	if err != nil || !ready {
		return s.wait()
	}
	if err = s.recoveryRetryBudget(ctx, t, c, i, seq); err != nil {
		return err
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	auth := loom.Auth{ServiceToken: service, SubjectToken: credential.Token}
	// Replaying the exact accepted message is safe; switching to /input is not.
	receipt, err := s.loom.RecoverMessage(ctx, auth, r.ChatID, t.ID.String(), r.Request)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	return s.acceptRecoveryReceipt(ctx, t, c, i, user, service, auth, receipt)
}

func (s *Service) recoveryRetryBudget(ctx context.Context, t *db.Task, c checkpoint, i intent, seq int64) error {
	r := c.Recovery
	if time.Now().Before(r.RetryUntil) {
		return nil
	}
	if seq > r.WakeSequence {
		r.WakeSequence = seq
		r.RetryUntil = time.Now().Add(recoveryRetryWindow)
		if err := s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
		return nil
	}
	out := s.result(ctx, i, structuredText(s.languageFor(ctx, i) == "ru",
		"Your message is saved. Send another message to retry after the blocker is resolved.",
		"Сообщение сохранено. После устранения причины отправь новое сообщение, чтобы повторить попытку."),
		fmt.Sprintf("recovery-wait:%d", r.WakeSequence))
	if err := s.save(ctx, t, c, out); err != nil {
		return s.wait()
	}
	if err := s.deliver(ctx, t, c); err != nil {
		return err
	}
	return s.wait()
}

func (s *Service) acceptRecoveryReceipt(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User,
	service string, auth loom.Auth, receipt loom.RecoveryReceipt) error {
	r := c.Recovery
	r.Receipt = &receipt
	if err := s.save(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	if receipt.Abandoned {
		return s.finish(ctx, t, c, i, stateCancelled, stateCancelled)
	}
	if !receipt.Delivered {
		return s.pendingRecoveryReceipt(ctx, t, c, i, user, service, auth, receipt)
	}
	return s.attachRecoveredRun(ctx, t, c, i, auth, receipt)
}

func (s *Service) pendingRecoveryReceipt(ctx context.Context, t *db.Task, c checkpoint, i intent, user db.User,
	service string, auth loom.Auth, receipt loom.RecoveryReceipt) error {
	if receipt.NextAction == "reauthorize_session" {
		if err := s.reauthorizeRecovery(ctx, t, &c, i, user, service, auth); err != nil {
			return err
		}
	}
	return s.waitWithRecoveryOutput(ctx, t, c, s.recoveryBlocker(ctx, i, receipt.Reason, receipt.NextAction))
}

func (s *Service) waitWithRecoveryOutput(ctx context.Context, t *db.Task, c checkpoint, out []db.Outbound) error {
	if err := s.save(ctx, t, c, out); err != nil {
		return s.wait()
	}
	if err := s.deliver(ctx, t, c); err != nil {
		return err
	}
	return s.wait()
}

func (s *Service) attachRecoveredRun(ctx context.Context, t *db.Task, c checkpoint, i intent,
	auth loom.Auth, receipt loom.RecoveryReceipt) error {
	run, err := s.loom.GetRun(ctx, auth, receipt.RunID)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	prior, err := s.store.RecoveryRunSession(ctx, t.UserID, run.ID, run.AuthSessionID)
	if err != nil {
		return s.wait()
	}
	var priorCheckpoint checkpoint
	if len(prior) > 0 {
		if err = json.Unmarshal(prior, &priorCheckpoint); err != nil {
			return s.wait()
		}
		if c.Session.ID != run.AuthSessionID || priorCheckpoint.Session.Version > c.Session.Version {
			c.Session = priorCheckpoint.Session
		}
		if priorCheckpoint.Run != nil && priorCheckpoint.Run.ID == run.ID {
			c.KeyboardPrompt = priorCheckpoint.KeyboardPrompt
			c.KeyboardRevision = priorCheckpoint.KeyboardRevision
		}
	}
	t.RunID, t.AuthSessionID = run.ID, run.AuthSessionID
	c.Run = &run
	snapshot, err := json.Marshal(c)
	if err != nil {
		return err
	}
	owned, observer, err := s.store.OwnRecoveredRun(ctx, *t, s.owner, snapshot)
	if err != nil {
		return s.wait()
	}
	if !observer {
		t.State = stateCompleted
		if err = s.save(ctx, t, c, s.recoveryBlocker(ctx, i, receipt.Reason, receipt.NextAction)); err != nil {
			return s.wait()
		}
		return s.deliverAndContinueStart(ctx, t, c, i)
	}
	*t = owned
	return s.observeRun(ctx, t, c, i, run)
}

func (s *Service) reauthorizeRecovery(
	ctx context.Context,
	t *db.Task,
	c *checkpoint,
	i intent,
	user db.User,
	service string,
	auth loom.Auth,
) error {
	located, err := s.loom.ActiveRun(ctx, auth, c.Recovery.ChatID)
	if err != nil {
		return s.loomError(ctx, t, *c, i, err)
	}
	if located.RunID == "" {
		return nil
	}
	run, err := s.loom.GetRun(ctx, auth, located.RunID)
	if err != nil {
		return s.loomError(ctx, t, *c, i, err)
	}
	raw, err := s.store.RecoveryRunSession(ctx, t.UserID, run.ID, run.AuthSessionID)
	if err != nil {
		return s.wait()
	}
	var prior checkpoint
	if len(raw) == 0 {
		return nil
	}
	if err = json.Unmarshal(raw, &prior); err != nil {
		return s.wait()
	}
	if prior.Session.ID != run.AuthSessionID {
		return nil
	}
	c.Session = prior.Session
	t.AuthSessionID = run.AuthSessionID
	// Force the existing reauthorization flow, retaining its unknown-outcome guard.
	c.Run = &loom.Run{Status: runWaitingAuth, Reason: reasonAuthorizationRequired}
	_, err = s.ensureSession(ctx, t, c, i, user, service)
	return err
}

func (s *Service) observeRecoveredRun(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	user db.User,
	service string,
) error {
	if c.Session.ID == t.AuthSessionID && t.AuthSessionID != "" {
		ready, err := s.ensureSession(ctx, t, &c, i, user, service)
		if !ready {
			return err
		}
	}
	credential, err := s.iam.UserToken(ctx, user.Subject)
	if err != nil {
		return s.identityError(ctx, t, c, i, err)
	}
	run, err := s.loom.GetRun(ctx, loom.Auth{ServiceToken: service, SubjectToken: credential.Token}, t.RunID)
	if err != nil {
		return s.loomError(ctx, t, c, i, err)
	}
	if run.Status == runWaitingAuth {
		out := s.recoveryBlocker(ctx, i, reasonAuthorizationRequired, "reauthorize_session")
		if err = s.save(ctx, t, c, out); err != nil {
			return s.wait()
		}
		if err = s.deliver(ctx, t, c); err != nil {
			return err
		}
		return s.wait()
	}
	if run.Status == runPaused && run.Reason == reasonShutdown {
		return s.resumeShutdown(ctx, t, c, i, loom.Auth{ServiceToken: service, SubjectToken: credential.Token}, run)
	}
	return s.observeRun(ctx, t, c, i, run)
}

func (s *Service) recoveryBlocker(ctx context.Context, i intent, reason, action string) []db.Outbound {
	if reason == "" {
		return nil
	}
	var en, ru string
	switch action {
	case "fund_wallet_then_retry":
		en = "Your budget is used. Add funds or wait for the next funding, then send a message to retry."
		ru = "Бюджет исчерпан. Пополни баланс или дождись следующего пополнения, " +
			"затем отправь сообщение для повтора."
	case "reauthorize_session":
		en = "Your message is saved. Authorization must be renewed before work can continue."
		ru = "Сообщение сохранено. Для продолжения нужно обновить авторизацию."
	case "select_allowed_model_or_restore_access":
		en = "Your message is saved. Select an available model or restore access to the current model."
		ru = "Сообщение сохранено. Выбери доступную модель или восстанови доступ к текущей."
	case "reconcile_predecessor":
		en = "Your message is saved. An administrator must check the previous action's outcome before work continues."
		ru = "Сообщение сохранено. Администратор должен проверить результат предыдущего действия " +
			"перед продолжением."
	case "retry_same_message":
		en = "Your message is saved. Waiting for the previous task to stop before continuing."
		ru = "Сообщение сохранено. Жду остановки предыдущей задачи перед продолжением."
	case "retry_earlier_message":
		en = "Your message is saved and is waiting for an earlier message to be processed."
		ru = "Сообщение сохранено и ожидает обработки более раннего сообщения."
	default:
		en = "Your message is saved. Work is blocked; contact an administrator to resolve it."
		ru = "Сообщение сохранено. Работа заблокирована; обратись к администратору для устранения причины."
	}
	return s.result(ctx, i, structuredText(s.languageFor(ctx, i) == "ru", en, ru), "recovery-blocked:"+reason+":"+action)
}
