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

// AdoptAssignment binds a transport operation to the actor's pending proposal.
// It preserves the original terms and expiry and supplies no approval.
func (s *Store) AdoptAssignment(ctx context.Context, scope EventScope, actor, operation string,
	args PassAssignment, proposal uuid.UUID,
) (AssignmentPreparation, error) {
	if proposal == uuid.Nil {
		return AssignmentPreparation{}, ErrConflict
	}
	return s.prepareAssignment(ctx, scope, actor, operation, args, proposal)
}

// Event precedes canonical proposal locks, as it does for assignment effects.
// Raw scoped locking also permits receipt recovery after sales close.
func lockAssignmentEvent(ctx context.Context, q *sqlc.Queries, scope EventScope, event uuid.UUID) error {
	_, err := q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
		ID: event, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	return err
}

func decodeAssignmentPreparation(row sqlc.ShuttleAssignmentPreparation) (AssignmentPreparation, error) {
	var result AssignmentPreparation
	err := json.Unmarshal(row.Document, &result)
	return result, err
}

func assignmentAdoptable(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttleAssignmentPreparation) error {
	if _, err := q.GetRegistrationOperation(ctx, sqlc.GetRegistrationOperationParams{
		BotID: row.BotID, OrganizationID: row.OrganizationID, Actor: row.Actor, OperationID: row.OperationID,
	}); err == nil {
		return ErrConflict
	} else if !errors.Is(err, pgx.ErrNoRows) {
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

func adoptAssignment(ctx context.Context, q *sqlc.Queries, scope EventScope, actor, operation string,
	proposal uuid.UUID, hash []byte,
) (AssignmentPreparation, error) {
	row, err := q.LockAssignmentProposal(ctx, sqlc.LockAssignmentProposalParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, ProposalID: proposal,
	})
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if !bytes.Equal(row.RequestHash, hash) {
		return AssignmentPreparation{}, ErrConflict
	}
	if err = assignmentAdoptable(ctx, q, row); err != nil {
		return AssignmentPreparation{}, err
	}
	result, err := decodeAssignmentPreparation(row)
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if err = q.InsertAssignmentAlias(ctx, sqlc.InsertAssignmentAliasParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor,
		OperationID: operation, CanonicalOperationID: row.OperationID,
	}); err != nil {
		return AssignmentPreparation{}, err
	}
	return result, nil
}

func replayAssignmentPreparation(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttleAssignmentPreparation,
	hash []byte, resume uuid.UUID,
) (AssignmentPreparation, error) {
	if !bytes.Equal(row.RequestHash, hash) {
		return AssignmentPreparation{}, ErrConflict
	}
	if resume == uuid.Nil {
		return decodeAssignmentPreparation(row)
	}
	locked, err := q.LockAssignmentCanonicalPreparation(ctx, sqlc.LockAssignmentCanonicalPreparationParams{
		BotID: row.BotID, OrganizationID: row.OrganizationID, Actor: row.Actor, OperationID: row.OperationID,
	})
	if err != nil {
		return AssignmentPreparation{}, err
	}
	if locked.ProposalID != resume {
		return AssignmentPreparation{}, ErrConflict
	}
	if err = assignmentAdoptable(ctx, q, locked); err != nil {
		return AssignmentPreparation{}, err
	}
	return decodeAssignmentPreparation(locked)
}

// The transport advisory lock is held. Canonical confirmation also takes the
// canonical operation lock before the event: the receipt namespace trigger uses
// that lock, so taking it after the event would invert concurrent confirmations.
func assignmentOperationForChange(ctx context.Context, q *sqlc.Queries, scope EventScope,
	actor, operation string, event, proposal uuid.UUID,
) (string, error) {
	row, err := q.GetAssignmentPreparation(ctx, sqlc.GetAssignmentPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, OperationID: operation,
	})
	if proposal == uuid.Nil {
		if err == nil {
			return "", ErrConflict
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return operation, nil
		}
		return "", err
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	if err != nil {
		return "", err
	}
	if row.OperationID != operation {
		if err = q.LockEventOperation(
			ctx,
			fmt.Sprintf("registration:%d:%s:%s:%s", scope.BotID, scope.Organization, actor, row.OperationID),
		); err != nil {
			return "", err
		}
	}
	if err = lockAssignmentEvent(ctx, q, scope, event); err != nil {
		return "", err
	}
	locked, err := q.LockAssignmentCanonicalPreparation(ctx, sqlc.LockAssignmentCanonicalPreparationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, OperationID: row.OperationID,
	})
	return locked.OperationID, err
}

// A fresh preparation or alias cannot reinterpret an unrelated committed receipt.
func requireUnusedRegistrationOperation(ctx context.Context, q *sqlc.Queries, scope EventScope,
	actor, operation string,
) error {
	_, err := q.GetRegistrationOperation(ctx, sqlc.GetRegistrationOperationParams{
		BotID: scope.BotID, OrganizationID: scope.Organization, Actor: actor, OperationID: operation,
	})
	if err == nil {
		return ErrConflict
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}
