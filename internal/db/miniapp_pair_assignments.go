package db

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// MiniAppPairAssignmentSession follows current global or event administrator
// authorization. The fence checks local invalidation and expiry, never IAM I/O
// under database locks. Bot and agent transports share the canonical operation.
type MiniAppPairAssignmentSession struct {
	UserID             uuid.UUID
	Bearer, Generation [sha256.Size]byte
	AuthorityUnchanged func() bool
}

// HasPairAssignmentAccess includes historical events whose module is disabled.
// The mutation engine still checks module availability before any new effect.
func (s *Store) HasPairAssignmentAccess(ctx context.Context, scope PaymentScope) (bool, error) {
	return s.q.HasPairAssignmentAccess(ctx, sqlc.HasPairAssignmentAccessParams{
		UserID: scope.UserID, BotID: scope.BotID, OrganizationID: scope.Organization,
		Subject: scope.Subject, GlobalAdmin: scope.GlobalAdmin,
	})
}

func (s *Store) PrepareMiniAppPairAssignment(ctx context.Context, scope PaymentScope, operation string,
	args PairAssignment, resume uuid.UUID, session MiniAppPairAssignmentSession,
) (PairAssignmentPreparation, error) {
	return s.preparePairAssignment(ctx, scope, operation, args, resume, &session)
}

func (s *Store) ConfirmMiniAppPairAssignment(ctx context.Context, scope PaymentScope, operation string,
	args PairAssignment, proposal uuid.UUID, session MiniAppPairAssignmentSession,
) (PairAssignmentResult, error) {
	return s.confirmPairAssignment(ctx, scope, operation, args, proposal, &session)
}

// Event authority locks precede the actor SHARE/session barrier. A repeated
// check after waits and before commit catches local IAM invalidation or expiry.
func authorizePairAssignmentMiniApp(ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	session *MiniAppPairAssignmentSession,
) error {
	if session == nil {
		return nil
	}
	if session.AuthorityUnchanged == nil || !session.AuthorityUnchanged() {
		return ErrPairDenied
	}
	if session.UserID != scope.UserID {
		return ErrMiniAppDenied
	}
	owner, err := q.LockAuthorityActor(ctx, sqlc.LockAuthorityActorParams{
		BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: scope.Subject,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrMiniAppDenied
	}
	if err != nil {
		return err
	}
	if owner.ID != session.UserID {
		return ErrMiniAppDenied
	}
	if err = checkMiniAppActorSession(ctx, q, owner, session.Bearer, session.Generation); err != nil {
		return err
	}
	if !session.AuthorityUnchanged() {
		return ErrPairDenied
	}
	return nil
}

// Preparation, adoption and history share one transaction. A retry preserves
// the first journal timestamp; a locator never stores or grants human consent.
func finishPairAssignmentPreparation(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope PaymentScope,
	operation string, result PairAssignmentPreparation, session *MiniAppPairAssignmentSession,
) (PairAssignmentPreparation, error) {
	args := result.Arguments
	if err := recordNativePairAssignmentAction(ctx, q, session, operation, result.ID, "prepare",
		args.ExpectedPairVersion, args.ExpectedLeaderVersion, args.ExpectedFollowerVersion); err != nil {
		return PairAssignmentPreparation{}, err
	}
	if err := authorizePairAssignmentMiniApp(ctx, q, scope, session); err != nil {
		return PairAssignmentPreparation{}, err
	}
	return result, tx.Commit(ctx)
}

func finishPairAssignmentConfirmation(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope PaymentScope,
	transport string, proposal uuid.UUID, result PairAssignmentResult, session *MiniAppPairAssignmentSession,
) (PairAssignmentResult, error) {
	if err := recordNativePairAssignmentAction(ctx, q, session, transport, proposal, "confirm",
		result.Pair.Version, result.Leader.Version, result.Follower.Version); err != nil {
		return PairAssignmentResult{}, err
	}
	if err := authorizePairAssignmentMiniApp(ctx, q, scope, session); err != nil {
		return PairAssignmentResult{}, err
	}
	return result, tx.Commit(ctx)
}

func recordNativePairAssignmentAction(ctx context.Context, q *sqlc.Queries,
	session *MiniAppPairAssignmentSession, operation string, proposal uuid.UUID, kind string,
	pairVersion, leaderVersion, followerVersion int64,
) error {
	if session == nil {
		return nil
	}
	return q.InsertNativePairAssignmentAction(ctx, sqlc.InsertNativePairAssignmentActionParams{
		UserID: session.UserID, OperationID: operation, ProposalID: proposal, Kind: kind,
		PairVersion: pairVersion, LeaderVersion: leaderVersion, FollowerVersion: followerVersion,
	})
}
