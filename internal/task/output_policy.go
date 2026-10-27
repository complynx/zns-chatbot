package task

import (
	"context"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/loom"
)

func (s *Service) createLoomRun(
	ctx context.Context, t *db.Task, c *checkpoint, i intent, auth loom.Auth,
) (loom.Run, error) {
	// A saved creation body may predate this contract. Never change its replay semantics.
	if c.Create == nil || c.Create.OutputPolicy != nil && c.Create.OutputPolicy.DisallowCustomEmoji {
		if err := s.loom.RequireOutputValidation(ctx, auth); err != nil {
			return loom.Run{}, err
		}
	}
	if err := s.prepareCreate(ctx, t, c, i); err != nil {
		return loom.Run{}, err
	}
	return s.loom.CreateRun(ctx, auth, t.ID.String(), *c.Create)
}
