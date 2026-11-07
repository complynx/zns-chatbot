package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
)

// NativeAssignmentAction is an immutable locator, not a current proposal or receipt.
type NativeAssignmentAction struct {
	OperationID          string    `json:"operation_id"`
	CanonicalOperationID string    `json:"canonical_operation_id"`
	ProposalID           uuid.UUID `json:"proposal_id"`
	Kind                 string    `json:"kind"`
	RegistrationVersion  int64     `json:"registration_version"`
	RecordedAt           time.Time `json:"recorded_at"`
}

// recordNativeAssignmentAction shares the mutation transaction. Agent calls have no session.
func recordNativeAssignmentAction(ctx context.Context, q *sqlc.Queries,
	session *MiniAppAssignmentSession, operation string, proposal uuid.UUID, kind string, version int64,
) error {
	if session == nil {
		return nil
	}
	return q.InsertNativeAssignmentAction(ctx, sqlc.InsertNativeAssignmentActionParams{
		UserID: session.UserID, OperationID: operation, ProposalID: proposal,
		Kind: kind, RegistrationVersion: version,
	})
}

// HasNativeAssignmentContext checks actor-owned history before obtaining current IAM roles.
func (s *Store) HasNativeAssignmentContext(ctx context.Context, scope ProfileScope, taskID uuid.UUID) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return false, err
	}
	return s.q.WithTx(tx).HasNativeAssignmentContext(ctx, sqlc.HasNativeAssignmentContextParams{
		UserID: scope.UserID, TaskID: taskID, BotID: scope.BotID,
		OrganizationID: scope.Organization, Actor: scope.Subject,
	})
}

// RecentNativeAssignmentActions requires current global administrator permission.
// It returns five actor-owned locators before the task cutoff, never live results
// that could introduce later actions into historical context.
func (s *Store) RecentNativeAssignmentActions(ctx context.Context, scope ProfileScope,
	taskID uuid.UUID,
) ([]NativeAssignmentAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := s.q.WithTx(tx).RecentNativeAssignmentActions(ctx, sqlc.RecentNativeAssignmentActionsParams{
		UserID: scope.UserID, TaskID: taskID, BotID: scope.BotID,
		OrganizationID: scope.Organization, Actor: scope.Subject,
	})
	if err != nil {
		return nil, err
	}
	result := make([]NativeAssignmentAction, 0, len(rows))
	for _, row := range rows {
		result = append(result, NativeAssignmentAction{
			OperationID: row.OperationID, CanonicalOperationID: row.CanonicalOperationID,
			ProposalID: row.ProposalID, Kind: row.Kind, RegistrationVersion: row.RegistrationVersion,
			RecordedAt: row.CreatedAt.Time.UTC(),
		})
	}
	return result, nil
}
