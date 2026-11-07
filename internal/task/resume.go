package task

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

// resumeShutdown continues an accepted run after Loom shutdown, never a client pause.
// The checkpoint pins the key before dispatch so a lost receipt cannot create another run.
func (s *Service) resumeShutdown(
	ctx context.Context,
	t *db.Task,
	c checkpoint,
	i intent,
	auth loom.Auth,
	run loom.Run,
) error {
	t.RunID = run.ID
	c.Run = &run
	t.State = stateAwaitingService
	if run.Version <= 0 {
		if err := s.save(ctx, t, c, nil); err != nil {
			return s.wait()
		}
		return s.wait()
	}
	if c.ResumeKey == "" {
		c.ResumeKey = t.ID.String() + ":resume:" + strconv.FormatInt(run.Version, 10)
	}
	if err := s.save(ctx, t, c, nil); err != nil {
		return s.wait()
	}
	_, err := s.loom.Resume(ctx, auth, run.ID, c.ResumeKey)
	if err != nil {
		rejected, known := errors.AsType[*loom.Error](err)
		if known && rejected.Outcome == loom.Rejected && rejected.StatusCode == http.StatusConflict {
			// Loom may still be reconciling an uncertain provider attempt. Do not replace it.
			c.LoomAuthRetryUntil = nil
			if saveErr := s.save(ctx, t, c, nil); saveErr != nil {
				return s.wait()
			}
			return s.wait()
		}
		return s.loomError(ctx, t, c, i, err)
	}
	c.ResumeKey = ""
	c.LoomAuthRetryUntil = nil
	// Control receipts omit result/question/approval; only a fresh run read may drive delivery.
	if saveErr := s.save(ctx, t, c, nil); saveErr != nil {
		return s.wait()
	}
	return s.wait()
}
