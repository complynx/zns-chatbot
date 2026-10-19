package db

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// MiniAppPairSession is checked after event/owner locks and before receipt replay.
// The caller must authenticate IAM first; business and session validation share one transaction.
type MiniAppPairSession struct{ Bearer, Generation [sha256.Size]byte }

func (s *Store) CreateMiniAppPairInvitation(ctx context.Context, scope PaymentScope,
	session MiniAppPairSession, operation string, args PairInvitationRequest,
) (PairResult, error) {
	return s.createPairInvitationOperation(ctx, scope, operation, args, &session)
}

func (s *Store) DecideMiniAppPairInvitation(ctx context.Context, scope PaymentScope,
	session MiniAppPairSession, operation, decision string, args PairInvitationDecision,
) (PairResult, error) {
	return s.decidePairInvitationOperation(ctx, scope, operation, decision, args, &session)
}

// The caller already holds the actor SHARE lock. Lock only the session: upgrading
// the owner lock would deadlock concurrent operations across different events.
func authorizePairMiniApp(
	ctx context.Context,
	q *sqlc.Queries,
	owner sqlc.ShuttleUser,
	session *MiniAppPairSession,
) error {
	if session == nil {
		return nil
	}
	initial, err := q.FindMiniAppBearer(
		ctx,
		sqlc.FindMiniAppBearerParams{BotID: owner.BotID, BearerHash: session.Bearer[:]},
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
	if row.State != miniAppActive || !hmac.Equal(row.BearerHash, session.Bearer[:]) ||
		!miniAppCurrent(row, owner, session.Generation, now.Time) {
		return ErrMiniAppDenied
	}
	return nil
}

// DissolveMiniAppPair uses the agent operation with session validation before any receipt replay.
func (s *Store) DissolveMiniAppPair(ctx context.Context, scope PaymentScope,
	session MiniAppPairSession, operation string, args PairDissolution,
) (registrations.Pair, error) {
	return s.dissolvePair(ctx, scope, operation, args, &session)
}
