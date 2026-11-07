package db

import (
	"context"
	"crypto/sha256"

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
	return checkMiniAppActorSession(ctx, q, actor, session.Bearer, session.Generation)
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
