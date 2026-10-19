package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
)

type MiniAppAvatarAdminSession struct {
	Bearer, Generation [sha256.Size]byte
	// AuthorityUnchanged is a local invalidation/expiry check, never IAM I/O.
	AuthorityUnchanged func() bool `json:"-"`
}

// The actor already has a SHARE lock. Do not use lockMiniAppBearer here: its
// owner FOR UPDATE lock can deadlock administrators repairing each other.
func lockAvatarAdminSession(
	ctx context.Context,
	q *sqlc.Queries,
	actor sqlc.ShuttleUser,
	session MiniAppAvatarAdminSession,
) error {
	if session.AuthorityUnchanged != nil && !session.AuthorityUnchanged() {
		return payments.ErrDenied
	}
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: actor.BotID, BearerHash: session.Bearer[:]},
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrMiniAppDenied
		}
		return err
	}
	if initial.UserID != actor.ID {
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
	if row.State != miniAppActive || !hmac.Equal(row.BearerHash, session.Bearer[:]) ||
		!miniAppCurrent(row, actor, session.Generation, now.Time) {
		return ErrMiniAppDenied
	}
	return nil
}

// Actor and session locks are already held. Rechecking before commit fences
// expiry that passes while waiting for a proposal or recipient row.
func refreshAvatarAdminSession(
	ctx context.Context,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	session MiniAppAvatarAdminSession,
) error {
	actor, err := q.LockAuthorityActor(
		ctx,
		sqlc.LockAuthorityActorParams{
			BotID:          scope.BotID,
			ZitadelOrg:     scope.Organization,
			ZitadelSubject: scope.Subject,
		},
	)
	if err != nil {
		return err
	}
	if actor.ID != scope.UserID {
		return ErrMiniAppDenied
	}
	return lockAvatarAdminSession(ctx, q, actor, session)
}
