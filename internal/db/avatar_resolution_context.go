package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

type NativeAvatarResolutionAction struct {
	OperationID          uuid.UUID `json:"operation_id"`
	CanonicalOperationID uuid.UUID `json:"canonical_operation_id"`
	ProposalID           uuid.UUID `json:"proposal_id"`
	DeliveryID           uuid.UUID `json:"delivery_id"`
	Kind                 string    `json:"kind"`
	DeliveryVersion      int64     `json:"delivery_version"`
	RecordedAt           time.Time `json:"recorded_at"`
}

// HasNativeAvatarResolutionContext returns only actor-owned history presence.
// It avoids IAM role lookups for users
// without native administrator actions before this task.
func (s *Store) HasNativeAvatarResolutionContext(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return false, err
	}
	return s.q.WithTx(tx).
		HasNativeAvatarResolutionContext(ctx, sqlc.HasNativeAvatarResolutionContextParams{UserID: scope.UserID, TaskID: taskID, BotID: scope.BotID, OrganizationID: scope.Organization, Actor: scope.Subject})
}

// RecentNativeAvatarResolutionActions requires current global administrator
// authority. It returns five immutable action locators before the task cutoff,
// never live proposal results that could introduce a later action into history.
func (s *Store) RecentNativeAvatarResolutionActions(
	ctx context.Context,
	scope AvatarDeliveryAdminScope,
	taskID uuid.UUID,
) ([]NativeAvatarResolutionAction, error) {
	if err := validateAvatarProposalScope(scope); err != nil {
		return nil, err
	}
	tx, q, err := s.beginAvatarResolution(ctx, scope, uuid.Nil)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := q.RecentNativeAvatarResolutionActions(
		ctx,
		sqlc.RecentNativeAvatarResolutionActionsParams{
			UserID:         scope.UserID,
			TaskID:         taskID,
			BotID:          scope.BotID,
			OrganizationID: scope.Organization,
			Actor:          scope.Subject,
		},
	)
	if err != nil {
		return nil, err
	}
	result := make([]NativeAvatarResolutionAction, 0, len(rows))
	for _, row := range rows {
		result = append(
			result,
			NativeAvatarResolutionAction{
				OperationID:          row.OperationID,
				CanonicalOperationID: row.CanonicalOperationID,
				ProposalID:           row.ProposalID,
				DeliveryID:           row.DeliveryID,
				Kind:                 row.Kind,
				DeliveryVersion:      row.DeliveryVersion,
				RecordedAt:           row.CreatedAt.Time.UTC(),
			},
		)
	}
	return result, nil
}
