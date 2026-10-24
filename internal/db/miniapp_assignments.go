package db

import (
	"context"
	"crypto/sha256"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/payments"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

// MiniAppAssignmentSession is supplied after current global administrator
// authorization. AuthorityUnchanged must check only local invalidation/expiry;
// network IAM work belongs before the transaction, never under database locks.
type MiniAppAssignmentSession struct {
	UserID             uuid.UUID
	Bearer, Generation [sha256.Size]byte
	AuthorityUnchanged func() bool
}

// PrepareMiniAppAssignment preserves the same proposal and receipt namespace
// used by the agent. A resume proposal adopts terms, never supplies consent.
func (s *Store) PrepareMiniAppAssignment(ctx context.Context, scope EventScope, actor, operation string,
	args PassAssignment, resume uuid.UUID, session MiniAppAssignmentSession,
) (AssignmentPreparation, error) {
	return s.prepareAssignment(ctx, scope, actor, operation, args, resume, &session)
}

// ConfirmMiniAppAssignment requires explicit confirmation of the exact proposal.
// Session checks also apply to completed receipts, including after restart.
func (s *Store) ConfirmMiniAppAssignment(ctx context.Context, scope EventScope, actor, operation string,
	args PassAssignment, proposal uuid.UUID, session MiniAppAssignmentSession,
) (registrations.Registration, error) {
	if proposal == uuid.Nil {
		return registrations.Registration{}, ErrConflict
	}
	return s.changeRegistration(ctx, scope, actor, operation, args.change(), proposal, nil, &session)
}

// Event locking precedes this actor/session barrier. Recheck it after waits and
// before commit so expiry or local IAM invalidation cannot authorize new effects.
func authorizeAssignmentMiniApp(ctx context.Context, q *sqlc.Queries, scope EventScope,
	actor string, session *MiniAppAssignmentSession,
) error {
	if session == nil {
		return nil
	}
	if session.AuthorityUnchanged == nil || !session.AuthorityUnchanged() {
		return payments.ErrDenied
	}
	owner, err := q.LockAuthorityActor(ctx, sqlc.LockAuthorityActorParams{
		BotID: scope.BotID, ZitadelOrg: scope.Organization, ZitadelSubject: actor,
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
		return payments.ErrDenied
	}
	return nil
}

// Native preparation and its history locator commit together; transport retries
// reuse the journal key and preserve its original timestamp.
func finishAssignmentPreparation(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope EventScope,
	actor, operation string, result AssignmentPreparation, session *MiniAppAssignmentSession,
) (AssignmentPreparation, error) {
	if err := recordNativeAssignmentAction(
		ctx,
		q,
		session,
		operation,
		result.ID,
		"prepare",
		result.Arguments.ExpectedVersion,
	); err != nil {
		return AssignmentPreparation{}, err
	}
	if err := authorizeAssignmentMiniApp(ctx, q, scope, actor, session); err != nil {
		return AssignmentPreparation{}, err
	}
	return result, tx.Commit(ctx)
}

func finishAssignmentConfirmation(ctx context.Context, tx pgx.Tx, q *sqlc.Queries, scope EventScope,
	actor, transport string, proposal uuid.UUID, result registrations.Registration, session *MiniAppAssignmentSession,
) (registrations.Registration, error) {
	if err := recordNativeAssignmentAction(
		ctx,
		q,
		session,
		transport,
		proposal,
		"confirm",
		result.Version,
	); err != nil {
		return registrations.Registration{}, err
	}
	if err := authorizeAssignmentMiniApp(ctx, q, scope, actor, session); err != nil {
		return registrations.Registration{}, err
	}
	return result, tx.Commit(ctx)
}
