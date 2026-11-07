package db

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// AdoptAvatarDeliveryResolution binds a new transport operation to the actor's
// existing pending proposal. Adoption neither confirms it nor extends its expiry.
func (s *Store) AdoptAvatarDeliveryResolution(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	op, proposal uuid.UUID,
	a AvatarDeliveryResolution,
) (AvatarDeliveryPreparation, error) {
	if proposal == uuid.Nil {
		return AvatarDeliveryPreparation{}, ErrInvalidAvatar
	}
	return s.prepareAvatarDeliveryResolution(ctx, scope, op, proposal, a, nil)
}

func avatarResolutionAdoptable(ctx context.Context, q *sqlc.Queries, row sqlc.ShuttleAvatarDeliveryResolution) error {
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return err
	}
	if len(row.Result) != 0 || !now.Time.Before(row.ExpiresAt.Time) {
		return ErrConflict
	}
	return nil
}

func adoptAvatarDeliveryResolution(
	ctx context.Context,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	op, proposal uuid.UUID,
	hash []byte,
) (AvatarDeliveryPreparation, error) {
	row, err := q.LockAvatarDeliveryResolutionProposal(
		ctx,
		sqlc.LockAvatarDeliveryResolutionProposalParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			ProposalID:     proposal,
		},
	)
	if err != nil {
		return AvatarDeliveryPreparation{}, err
	}
	result, err := avatarResolutionPreparation(ctx, q, row, hash, proposal, false)
	if err != nil {
		return result, err
	}
	err = q.InsertAvatarDeliveryResolutionAlias(
		ctx,
		sqlc.InsertAvatarDeliveryResolutionAliasParams{
			BotID:                scope.BotID,
			OrganizationID:       scope.Organization,
			Actor:                scope.Subject,
			OperationID:          op,
			CanonicalOperationID: row.OperationID,
		},
	)
	return result, err
}

// All confirmations lock the canonical row before checking its result. Operation
// advisory locks alone cannot serialize two different transport aliases.
func lockAvatarDeliveryResolution(
	ctx context.Context,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
) (sqlc.ShuttleAvatarDeliveryResolution, error) {
	row, err := q.GetAvatarDeliveryResolution(ctx, avatarResolutionKey(scope, op))
	if err != nil {
		return row, err
	}
	return q.LockAvatarDeliveryCanonicalResolution(
		ctx,
		sqlc.LockAvatarDeliveryCanonicalResolutionParams{
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
			OperationID:    row.OperationID,
		},
	)
}

// Both a fresh adoption and an exact prepare replay bind the same original document.
func avatarResolutionPreparation(
	ctx context.Context,
	q *sqlc.Queries,
	row sqlc.ShuttleAvatarDeliveryResolution,
	hash []byte,
	resume uuid.UUID,
	native bool,
) (AvatarDeliveryPreparation, error) {
	if native || resume != uuid.Nil {
		var err error
		row, err = q.LockAvatarDeliveryCanonicalResolution(
			ctx,
			sqlc.LockAvatarDeliveryCanonicalResolutionParams{
				BotID:          row.BotID,
				OrganizationID: row.OrganizationID,
				Actor:          row.Actor,
				OperationID:    row.OperationID,
			},
		)
		if err != nil {
			return AvatarDeliveryPreparation{}, err
		}
	}
	if native && resume == uuid.Nil {
		resume = row.ProposalID
	}
	if !bytes.Equal(row.RequestHash, hash) || (resume != uuid.Nil && row.ProposalID != resume) {
		return AvatarDeliveryPreparation{}, ErrConflict
	}
	if resume != uuid.Nil {
		if err := avatarResolutionAdoptable(ctx, q, row); err != nil {
			return AvatarDeliveryPreparation{}, err
		}
	}
	var result AvatarDeliveryPreparation
	err := json.Unmarshal(row.Document, &result)
	return result, err
}
