package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type MiniAppProfileAction struct {
	OperationID    uuid.UUID `json:"operation_id"`
	Kind           string    `json:"kind"`
	ProfileVersion int64     `json:"profile_version"`
	CompletedAt    time.Time `json:"completed_at"`
}

// RecentMiniAppProfileActions returns successful owner actions before this user-triggered task.
// The receipt supplies the historical version; names and session credentials are not copied.
func (s *Store) RecentMiniAppProfileActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]MiniAppProfileAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT a.operation_id,a.kind,(op.result->>'version')::bigint,a.completed_at
FROM shuttle_tasks current_task JOIN shuttle_miniapp_profile_actions a ON a.user_id=current_task.user_id
JOIN shuttle_profile_operations op ON op.user_id=a.user_id AND op.operation_id=a.operation_id
WHERE current_task.id=$1 AND current_task.user_id=$2 AND a.completed_at<current_task.created_at
ORDER BY a.completed_at DESC,a.operation_id LIMIT 5`, taskID, scope.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	actions := make([]MiniAppProfileAction, 0)
	for rows.Next() {
		var action MiniAppProfileAction
		if err = rows.Scan(&action.OperationID, &action.Kind, &action.ProfileVersion, &action.CompletedAt); err != nil {
			return nil, err
		}
		action.CompletedAt = action.CompletedAt.UTC()
		actions = append(actions, action)
	}
	return actions, rows.Err()
}
