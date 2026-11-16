package task

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

var errRecoveryAuthorizationPending = errors.New("observer is renewing authorization")

func (s *Service) failedRunReadOutput(ctx context.Context, c checkpoint, i intent) []db.Outbound {
	if c.RunLocation != nil && c.RunLocation.Reason != "" {
		if out := s.recoveryBlocker(ctx, i, c.RunLocation.Reason, c.RunLocation.NextAction); len(out) > 0 {
			return out
		}
	}
	return s.recoveryBlocker(ctx, i, "service_unavailable", "restore_service_then_retry")
}

func (s *Service) requestAccessBlocker(ctx context.Context, t db.Task, c checkpoint, i intent) []db.Outbound {
	if c.Recovery == nil && t.RunID == "" && t.TargetRunID == "" {
		return s.recoveryBlocker(ctx, i, "agent_access_required", "restore_agent_access")
	}
	return s.recoveryBlocker(ctx, i, "conversation_access_required", actionRestoreChatAccess)
}

func rejectedRecovery(err error, status int, code string) bool {
	upstream, ok := errors.AsType[*loom.Error](err)
	return ok && upstream.Outcome == loom.Rejected && upstream.StatusCode == status && upstream.Code == code
}

func (s *Service) recoveryTargetReady(ctx context.Context, t db.Task) error {
	active, err := s.store.RecoveryObserver(ctx, t)
	if err != nil {
		return err
	}
	if err = recoveryAuthorizationReady(active); err != nil {
		return err
	}
	if t.TargetTaskID == uuid.Nil || t.TargetTaskID == active.ID {
		return nil
	}
	prior, err := s.store.GetTask(ctx, t.TargetTaskID)
	if err != nil {
		return err
	}
	return recoveryAuthorizationReady(prior)
}

func recoveryAuthorizationReady(prior db.Task) error {
	// A terminal observer no longer owns new input. Keep its renewal evidence
	// for audit; the new task still requires current customer authorization.
	if prior.ID == uuid.Nil || terminal(prior.State) {
		return nil
	}
	var saved checkpoint
	if err := json.Unmarshal(prior.Snapshot, &saved); err != nil {
		return err
	}
	if prior.State == stateAuthUnknown {
		return db.ErrRoutingPending
	}
	if saved.Reauthorizing {
		return errRecoveryAuthorizationPending
	}
	return nil
}

func (s *Service) recoveryTargetError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	if errors.Is(err, errRecoveryAuthorizationPending) {
		return s.waitForRecoveryOrder(*t)
	}
	if !errors.Is(err, db.ErrRoutingPending) {
		return s.wait()
	}
	return s.parkRecoveryOutput(ctx, t, c,
		s.recoveryBlocker(ctx, i, "authorization_outcome_unknown", actionReconcilePredecessor))
}

func (s *Service) recoveryLocatorError(ctx context.Context, t *db.Task, c checkpoint, i intent,
	failure error) (bool, error) {
	if !rejectedRecovery(failure, http.StatusNotFound, "not_found") {
		return true, s.recoveryCapabilityError(ctx, t, c, i, failure)
	}
	if t.TargetRunID != "" {
		prior, err := s.store.GetTask(ctx, t.TargetTaskID)
		if err != nil {
			return true, s.wait()
		}
		if !terminal(prior.State) {
			t.State = stateAwaitingService
			return true, s.parkRecoveryOutput(ctx, t, c,
				s.recoveryBlocker(ctx, i, "conversation_access_required", actionRestoreChatAccess))
		}
	}
	// Another actor can own the latest terminal run. Ordinary creation checks
	// chat ownership and admission without disclosing that run.
	if c.Create == nil {
		return false, nil
	}
	return true, s.parkRecoveryOutput(ctx, t, c,
		s.recoveryBlocker(ctx, i, "actor_access_required", "contact_original_actor"))
}

func (s *Service) recoveryChat(ctx context.Context, t db.Task, auth loom.Auth) (string, error) {
	if t.TargetRunID != "" {
		prior, err := s.store.GetTask(ctx, t.TargetTaskID)
		if err != nil {
			return "", err
		}
		var saved checkpoint
		if prior.UserID == t.UserID && json.Unmarshal(prior.Snapshot, &saved) == nil &&
			saved.Run != nil && saved.Run.ChatID != "" {
			return saved.Run.ChatID, nil
		}
	}
	return s.loom.DefaultChat(ctx, auth)
}

func (s *Service) recoveryMessageError(ctx context.Context, t *db.Task, c checkpoint, i intent,
	auth loom.Auth, failure error) error {
	r := c.Recovery
	if r.Receipt == nil && rejectedRecovery(failure, http.StatusConflict, "no_run") {
		// The inbox checks the retained key before admission. No run means
		// this request was not accepted; rediscover without changing task identity.
		c.Recovery = nil
		return s.waitWithRecoveryOutput(ctx, t, c, nil)
	}
	if r.Receipt == nil && rejectedRecovery(failure, http.StatusConflict, "replacement_target_changed") {
		// Loom checks the retained key before validating the target. This typed
		// rejection proves that no version of this message was accepted.
		located, err := s.loom.ActiveRun(ctx, auth, r.ChatID)
		if err != nil {
			return s.loomError(ctx, t, c, i, err)
		}
		replacement, finished, err := s.recoveryReplacement(ctx, auth, located)
		if err != nil {
			return s.loomError(ctx, t, c, i, err)
		}
		if finished {
			c.Recovery = nil
			return s.waitWithRecoveryOutput(ctx, t, c, nil)
		}
		r.Request.ReplaceRunID = replacement
		r.ObservedRunID = located.RunID
		return s.waitWithRecoveryOutput(ctx, t, c, nil)
	}
	if r.Receipt == nil && rejectedRecovery(failure, http.StatusUnprocessableEntity, "invalid_message") {
		r.Rejected = true
		t.State = stateFailed
		out := s.result(ctx, i, structuredText(s.languageFor(ctx, i) == "ru",
			"The agent rejected the message or saved context. "+
				"Edit the request or ask an administrator to check it.",
			"Агент не принял сообщение или сохранённый контекст. "+
				"Измени запрос или попроси администратора проверить его."), "recovery-invalid-message")
		if err := s.save(ctx, t, c, out); err != nil {
			return s.wait()
		}
		return s.deliverAndContinueStart(ctx, t, c, i)
	}
	return s.loomError(ctx, t, c, i, failure)
}

// A failed read is not a completion receipt. After a bounded fast retry window,
// rediscover the retained run and slow reads down without sending any new input.
func (s *Service) readConversationRun(ctx context.Context, c *checkpoint,
	auth loom.Auth, id string) (loom.Run, error) {
	if c.RunReadNextCheck.After(time.Now()) {
		return loom.Run{}, &loom.Error{Outcome: loom.Rejected, Retryable: true, Code: "service_unavailable"}
	}
	if c.RunReadRetryUntil != nil && !time.Now().Before(*c.RunReadRetryUntil) {
		c.RunReadNextCheck = time.Now().Add(time.Minute)
		c.RunLocation = nil
		located, err := s.rediscoverObservedRun(ctx, *c, auth)
		if err == nil && located.RunID == id {
			c.RunLocation = &located
		}
		// Discovery is advisory. A locator can hide another actor's latest
		// run while this caller can still read the pinned terminal result.
	}
	run, err := s.loom.GetRun(ctx, auth, id)
	if err == nil {
		c.RunReadRetryUntil = nil
		c.RunReadNextCheck = time.Time{}
		c.RunLocation = nil
		return run, nil
	}
	upstream, known := errors.AsType[*loom.Error](err)
	if known && upstream.StatusCode != http.StatusUnauthorized && c.RunReadRetryUntil == nil {
		until := time.Now().Add(recoveryRetryWindow)
		c.RunReadRetryUntil = &until
	}
	return run, err
}

func (s *Service) rediscoverObservedRun(ctx context.Context, c checkpoint, auth loom.Auth) (loom.ActiveRun, error) {
	supported, err := s.loom.SupportsConversationRecovery(ctx, auth)
	if err != nil || !supported {
		return loom.ActiveRun{}, err
	}
	chat := ""
	if c.Run != nil {
		chat = c.Run.ChatID
	}
	if c.Recovery != nil {
		chat = c.Recovery.ChatID
	}
	if chat == "" {
		chat, err = s.loom.DefaultChat(ctx, auth)
		if err != nil {
			return loom.ActiveRun{}, err
		}
	}
	// A later run does not disprove this observer's unread result. Rediscover
	// the chat, then reconcile the pinned run; only a delivered message may
	// transfer ownership. This read path never repeats original input.
	return s.loom.ActiveRun(ctx, auth, chat)
}
