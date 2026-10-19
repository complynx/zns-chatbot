package db

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// AdoptPairAssignment binds a new transport key to this actor's pending proposal.
// Original recipients, terms and expiry remain unchanged; adoption is not approval.
func (s *Store) AdoptPairAssignment(ctx context.Context, scope PaymentScope, operation string,
	args PairAssignment, proposal uuid.UUID,
) (PairAssignmentPreparation, error) {
	if proposal == uuid.Nil {
		return PairAssignmentPreparation{}, ErrConflict
	}
	return s.preparePairAssignment(ctx, scope, operation, args, proposal)
}

func decodePairAssignmentPreparation(row sqlc.ShuttlePairPreparation) (PairAssignmentPreparation, error) {
	var result PairAssignmentPreparation
	err := json.Unmarshal(row.Document, &result)
	return result, err
}

func pairAssignmentAdoptable(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttlePairPreparation) error {
	_, err := q.GetPairOperation(ctx, sqlc.GetPairOperationParams{
		BotID: row.BotID, OrganizationID: row.OrganizationID, Actor: row.Actor, OperationID: row.OperationID,
	})
	if err == nil {
		return ErrConflict
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if !now.Time.Before(row.ExpiresAt.Time) {
		return ErrConflict
	}
	return nil
}

func requireUnusedPairAssignmentOperation(
	ctx context.Context,
	q *sqlc.Queries,
	scope PaymentScope,
	operation string,
) error {
	_, err := q.GetPairOperation(ctx, sqlc.GetPairOperationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: operation,
	})
	if err == nil {
		return ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// Event and actor locks are already held. Only this transport key is inserted;
// the canonical preparation remains immutable and cannot itself be an alias.
func adoptPairAssignment(ctx context.Context, q *sqlc.Queries, scope PaymentScope, operation string,
	proposal uuid.UUID, hash []byte,
) (PairAssignmentPreparation, error) {
	row, err := q.LockPairAssignmentProposal(ctx, sqlc.LockPairAssignmentProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, ProposalID: proposal,
	})
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	if !bytes.Equal(row.RequestHash, hash) {
		return PairAssignmentPreparation{}, ErrConflict
	}
	if err = pairAssignmentAdoptable(ctx, q, row); err != nil {
		return PairAssignmentPreparation{}, err
	}
	result, err := decodePairAssignmentPreparation(row)
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	if err = q.InsertPairAssignmentAlias(ctx, sqlc.InsertPairAssignmentAliasParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject,
		OperationID: operation, CanonicalOperationID: row.OperationID,
	}); err != nil {
		return PairAssignmentPreparation{}, err
	}
	return result, nil
}

func replayPairAssignmentPreparation(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttlePairPreparation,
	hash []byte, resume uuid.UUID,
) (PairAssignmentPreparation, error) {
	if !bytes.Equal(row.RequestHash, hash) {
		return PairAssignmentPreparation{}, ErrConflict
	}
	if resume == uuid.Nil {
		return decodePairAssignmentPreparation(row)
	}
	locked, err := q.LockPairAssignmentCanonicalPreparation(ctx, sqlc.LockPairAssignmentCanonicalPreparationParams{
		BotID: row.BotID, OrganizationID: row.OrganizationID, Actor: row.Actor, OperationID: row.OperationID,
	})
	if err != nil {
		return PairAssignmentPreparation{}, err
	}
	if locked.ProposalID != resume {
		return PairAssignmentPreparation{}, ErrConflict
	}
	if err = pairAssignmentAdoptable(ctx, q, locked); err != nil {
		return PairAssignmentPreparation{}, err
	}
	return decodePairAssignmentPreparation(locked)
}

// The receipt namespace trigger also acquires the canonical advisory lock.
// Acquire it before the event to avoid inversion with a concurrent original-key
// confirmation; lock the actual preparation only after the authority barrier.
func lockPairAssignmentConfirmation(ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	operation string, event uuid.UUID,
) (sqlc.ShuttlePairPreparation, error) {
	lock := func(key string) error {
		return q.LockEventOperation(
			ctx,
			fmt.Sprintf("pair:%d:%s:%s:%s", scope.BotID, scope.Organization, scope.Subject, key),
		)
	}
	if err := lock(operation); err != nil {
		return sqlc.ShuttlePairPreparation{}, err
	}
	row, err := q.GetPairPreparation(ctx, sqlc.GetPairPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: operation,
	})
	if err != nil {
		return sqlc.ShuttlePairPreparation{}, err
	}
	if row.OperationID != operation {
		if err = lock(row.OperationID); err != nil {
			return sqlc.ShuttlePairPreparation{}, err
		}
	}
	if err = lockPairAssignmentAuthority(ctx, q, scope, event); err != nil {
		return sqlc.ShuttlePairPreparation{}, err
	}
	return q.LockPairAssignmentCanonicalPreparation(ctx, sqlc.LockPairAssignmentCanonicalPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: row.OperationID,
	})
}

// Participant operations retain exact receipt lookup. A fresh participant action
// must not reuse an assignment's canonical or alias key before applying effects.
func requireUnreservedPairParticipantOperation(ctx context.Context, q *sqlc.Queries,
	scope PaymentScope, operation string,
) error {
	_, err := q.GetPairPreparation(ctx, sqlc.GetPairPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject, OperationID: operation,
	})
	if err == nil {
		return ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
