package db

import (
	"context"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// A Telegram replay must not expose an accepted task after access was revoked.
func existingActiveTask(ctx context.Context, q *sqlc.Queries, id uuid.UUID) (Task, bool, error) {
	existing, err := q.GetTask(ctx, id)
	if err != nil {
		return Task{}, false, err
	}
	owner, err := q.GetUser(ctx, existing.UserID)
	if err != nil {
		return Task{}, false, err
	}
	if owner.Revoked {
		return Task{}, false, &UserRevokedError{PublicReason: owner.BanPublicReason}
	}
	return task(existing), true, nil
}
