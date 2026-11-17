package task

import (
	"context"
	"encoding/json"
	"time"

	"github.com/riverqueue/river"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func runNeedsAuthorization(run loom.Run) bool {
	return run.Status == runWaitingAuth &&
		(run.Reason == reasonAuthorizationRequired || run.Reason == reasonRecoveryAuthorizationRequired)
}

// Another task may have renewed this exact session. Adopt only its durable,
// newer receipt; unknown renewal outcomes remain subject to reconciliation.
func (s *Service) refreshSessionReceipt(ctx context.Context, t *db.Task, c *checkpoint) (bool, error) {
	if t.AuthSessionID == "" {
		return false, nil
	}
	raw, err := s.store.RecoveryRunSession(ctx, t.UserID, t.RunID, t.AuthSessionID)
	if err != nil {
		s.reportDatabaseFailure(ctx, t.ID, "recovery_session_refresh", err)
		return false, s.wait()
	}
	var saved checkpoint
	if len(raw) == 0 {
		return false, nil
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		return false, s.wait()
	}
	if saved.Reauthorizing || saved.SessionVersionUnknown || saved.Session.ID != t.AuthSessionID ||
		saved.Session.Version <= c.Session.Version {
		return false, nil
	}
	c.Session = saved.Session
	c.SessionVersionUnknown = false
	t.State = stateAwaitingService
	if c.Run != nil && runNeedsAuthorization(*c.Run) {
		c.Run = nil
	}
	if err = s.save(ctx, t, *c, nil); err != nil {
		return false, s.wait()
	}
	return c.Session.ExpiresAt.After(time.Now().Add(sessionRenewSkew)), nil
}

// A lost non-idempotent renewal receipt cannot be reconstructed from a CAS
// conflict. Keep observing admitted work, but never guess a session version.
func (s *Service) parkUnknownSession(ctx context.Context, t *db.Task, c checkpoint, i intent) error {
	t.State = stateAwaitingAuth
	return s.parkRecoveryOutput(ctx, t, c,
		s.recoveryBlocker(ctx, i, "authorization_outcome_unknown", actionReconcilePredecessor))
}

func (s *Service) resolveUnknownSession(ctx context.Context, t *db.Task, c *checkpoint, i intent) (bool, error) {
	if ready, err := s.refreshSessionReceipt(ctx, t, c); ready || err != nil {
		return ready, err
	}
	return false, s.parkUnknownSession(ctx, t, *c, i)
}

func (s *Service) prepareObservationSession(ctx context.Context, t *db.Task, c *checkpoint,
	i intent, user db.User, service string) (bool, error) {
	if c.SessionVersionUnknown {
		_, err := s.refreshSessionReceipt(ctx, t, c)
		return err == nil, err
	}
	if c.Session.ID != t.AuthSessionID || t.AuthSessionID == "" {
		return true, nil
	}
	return s.ensureSession(ctx, t, c, i, user, service)
}

func (s *Service) waitForRecoveryOrder(t db.Task) error {
	if !t.CreatedAt.IsZero() && time.Since(t.CreatedAt) >= recoveryRetryWindow {
		return river.JobSnooze(recoveryBackoffLimit)
	}
	return s.wait()
}
