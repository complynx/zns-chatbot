package task

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func (s *Service) runRequestError(
	ctx context.Context, t *db.Task, c checkpoint, i intent, run loom.Run, err error,
) error {
	var upstream *loom.Error
	if t.RunID == "" && run.ID != "" && errors.As(err, &upstream) &&
		upstream.Code == loom.CodeUnsupportedReplyMode {
		// Creation succeeded. Retain ownership before attempting cleanup.
		t.RunID, t.State = run.ID, stateAwaitingService
		c.Run, c.ReplyModeMismatch = &run, true
		if saveErr := s.save(ctx, t, c, nil); saveErr != nil {
			return s.wait()
		}
		return s.wait()
	}
	return s.loomError(ctx, t, c, i, err)
}

// An incompatible created run stays owned until a fresh read confirms termination.
// Cancellation uses the original task identity, including after a lost receipt or restart.
func (s *Service) reconcileIncompatibleRun(
	ctx context.Context, t *db.Task, c checkpoint, i intent, auth loom.Auth,
) error {
	run, err := s.loom.GetRun(ctx, auth, t.RunID)
	if err != nil {
		return s.incompatibleRunError(ctx, t, c, i, err)
	}
	if run.AuthSessionID != "" && run.AuthSessionID != t.AuthSessionID {
		return s.incompatibleRunError(ctx, t, c, i, nil)
	}
	c.Run = &run
	if finishedRun(run.Status) {
		return s.finish(ctx, t, c, i, stateFailed, stateFailed)
	}
	t.State = stateAwaitingService
	if err = s.save(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	_, err = s.loom.Cancel(ctx, auth, t.RunID, t.ID.String()+":incompatible-reply-mode")
	if err != nil {
		return s.incompatibleRunError(ctx, t, c, i, err)
	}
	// A control receipt can acknowledge an unfinished cancellation; reread the run.
	return s.wait()
}

func (s *Service) incompatibleRunError(ctx context.Context, t *db.Task, c checkpoint, i intent, err error) error {
	var upstream *loom.Error
	if errors.As(err, &upstream) && upstream.Outcome == loom.Rejected &&
		upstream.StatusCode == http.StatusUnauthorized {
		return s.waitForLoomAuthorization(ctx, t, c, i)
	}
	// Neither a lost receipt nor a later unrelated rejection proves the run stopped.
	c.LoomAuthRetryUntil = nil
	t.State = stateAwaitingService
	_ = s.save(ctx, t, c, nil)
	return s.wait()
}
