package db

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

func (s *Store) PrepareMiniAppAvatarResolution(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	session MiniAppAvatarAdminSession,
	op, proposal uuid.UUID,
	a AvatarDeliveryResolution,
) (AvatarDeliveryPreparation, error) {
	return s.prepareAvatarDeliveryResolution(ctx, scope, op, proposal, a, &session)
}

func (s *Store) ConfirmMiniAppAvatarResolution(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	session MiniAppAvatarAdminSession,
	op, proposal uuid.UUID,
	a AvatarDeliveryResolution,
) (AvatarDeliveryResolutionResult, error) {
	return s.resolveAvatarDelivery(ctx, scope, op, a, proposal, &session)
}

func (s *Store) MiniAppAvatarResolutionProposal(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	session MiniAppAvatarAdminSession,
	selector AvatarDeliveryProposalSelector,
) (AvatarDeliveryProposalView, error) {
	return s.avatarDeliveryProposal(ctx, scope, selector, &session)
}

func (s *Store) PendingMiniAppAvatarResolutions(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	session MiniAppAvatarAdminSession,
	after uuid.UUID,
	limit int,
) (AvatarDeliveryProposalPage, error) {
	return s.pendingAvatarDeliveryProposals(ctx, scope, after, limit, &session)
}

func (s *Store) MiniAppAvatarAdminDelivery(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	session MiniAppAvatarAdminSession,
	id uuid.UUID,
) (AvatarDelivery, error) {
	if err := validateAvatarProposalScope(scope); err != nil {
		return AvatarDelivery{}, err
	}
	if id == uuid.Nil {
		return AvatarDelivery{}, ErrInvalidAvatar
	}
	tx, q, err := s.beginAvatarResolutionSession(ctx, scope, id, &session)
	if err != nil {
		return AvatarDelivery{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	row, err := q.GetAvatarDelivery(
		ctx,
		sqlc.GetAvatarDeliveryParams{ID: id, BotID: scope.BotID, ZitadelOrg: scope.Organization},
	)
	return avatarDelivery(row), err
}

func recordNativeAvatarResolution(
	ctx context.Context,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	op, proposal uuid.UUID,
	kind string,
	version int64,
) error {
	return q.InsertNativeAvatarResolutionAction(
		ctx,
		sqlc.InsertNativeAvatarResolutionActionParams{
			UserID:          scope.UserID,
			OperationID:     op,
			ProposalID:      proposal,
			Kind:            kind,
			DeliveryVersion: version,
		},
	)
}

func finishAvatarResolutionPreparation(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	op uuid.UUID,
	result AvatarDeliveryPreparation,
	session *MiniAppAvatarAdminSession,
) (AvatarDeliveryPreparation, error) {
	if session != nil {
		if err := refreshAvatarAdminSession(ctx, q, scope, *session); err != nil {
			return result, err
		}
		if err := recordNativeAvatarResolution(
			ctx,
			q,
			scope,
			op,
			result.ID,
			"prepare",
			result.Delivery.Version,
		); err != nil {
			return result, err
		}
	}
	return result, tx.Commit(ctx)
}

func finishAvatarResolutionConfirmation(
	ctx context.Context,
	tx pgx.Tx,
	q *sqlc.Queries,
	scope AvatarDeliveryAdminScope,
	op, proposal uuid.UUID,
	result AvatarDeliveryResolutionResult,
	session *MiniAppAvatarAdminSession,
) (AvatarDeliveryResolutionResult, error) {
	if session != nil {
		if err := refreshAvatarAdminSession(ctx, q, scope, *session); err != nil {
			return result, err
		}
		if err := recordNativeAvatarResolution(ctx, q, scope, op, proposal, "confirm", result.Version); err != nil {
			return result, err
		}
	}
	return result, tx.Commit(ctx)
}
