package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// The actor SHARE lock must precede this session-only lock. Upgrading the
// actor lock here would deadlock operations across events or administrators.
func checkMiniAppActorSession(ctx context.Context, q *sqlc.Queries, owner sqlc.ShuttleUser,
	bearer, generation [sha256.Size]byte,
) error {
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: owner.BotID, BearerHash: bearer[:]},
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMiniAppDenied
	}
	if err != nil {
		return err
	}
	if initial.UserID != owner.ID {
		return ErrMiniAppDenied
	}
	row, err := q.LockMiniAppSession(ctx, initial.ID)
	if err != nil {
		return err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if row.State != miniAppActive || !hmac.Equal(row.BearerHash, bearer[:]) ||
		!miniAppCurrent(row, owner, generation, now.Time) {
		return ErrMiniAppDenied
	}
	return nil
}
