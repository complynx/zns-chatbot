package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// NativePairAssignmentAction locates an original snapshot without asserting live state.
type NativePairAssignmentAction struct {
	OperationID          string    `json:"operation_id"`
	CanonicalOperationID string    `json:"canonical_operation_id"`
	ProposalID           uuid.UUID `json:"proposal_id"`
	EventID              uuid.UUID `json:"event_id"`
	PairID               uuid.UUID `json:"pair_id"`
	Kind                 string    `json:"kind"`
	PairVersion          int64     `json:"pair_version"`
	LeaderVersion        int64     `json:"leader_version"`
	FollowerVersion      int64     `json:"follower_version"`
	RecordedAt           time.Time `json:"recorded_at"`
}

// HasNativePairAssignmentContext checks owned history before obtaining current IAM roles.
func (s *Store) HasNativePairAssignmentContext(
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
	return s.q.WithTx(tx).HasNativePairAssignmentContext(ctx, sqlc.HasNativePairAssignmentContextParams{
		UserID: scope.UserID, TaskID: taskID, BotID: scope.BotID,
		OrganizationID: scope.Organization, Actor: scope.Subject,
	})
}

// RecentNativePairAssignmentActions returns at most five pre-task locators.
// Current global or event administrator authority is checked before the limit.
func (s *Store) RecentNativePairAssignmentActions(ctx context.Context, scope PaymentScope,
	taskID uuid.UUID,
) ([]NativePairAssignmentAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	owner := ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		UserID:       scope.UserID,
		Subject:      scope.Subject,
	}
	if err = lockProfileOwner(ctx, tx, owner, false); err != nil {
		return nil, err
	}
	rows, err := s.q.WithTx(tx).RecentNativePairAssignmentActions(ctx, sqlc.RecentNativePairAssignmentActionsParams{
		UserID: scope.UserID, TaskID: taskID, BotID: scope.BotID,
		OrganizationID: scope.Organization, Actor: scope.Subject, GlobalAdmin: scope.GlobalAdmin,
	})
	if err != nil {
		return nil, err
	}
	actions := make([]NativePairAssignmentAction, 0, len(rows))
	for _, row := range rows {
		actions = append(actions, NativePairAssignmentAction{
			OperationID: row.OperationID, CanonicalOperationID: row.CanonicalOperationID,
			ProposalID: row.ProposalID, EventID: row.EventID, PairID: row.PairID, Kind: row.Kind,
			PairVersion: row.PairVersion, LeaderVersion: row.LeaderVersion, FollowerVersion: row.FollowerVersion,
			RecordedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return actions, nil
}
