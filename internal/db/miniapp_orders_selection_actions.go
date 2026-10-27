package db

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type MiniAppOrdersSelectionAction struct {
	OperationID    uuid.UUID `json:"operation_id"`
	EventID        uuid.UUID `json:"event_id"`
	DraftVersion   int64     `json:"draft_version"`
	EventVersion   int64     `json:"event_version"`
	CatalogVersion int64     `json:"catalog_version"`
	Cleared        bool      `json:"cleared"`
	CompletedAt    time.Time `json:"completed_at"`
}

// RecentMiniAppOrdersSelectionActions supplies bounded historical metadata only
// before this user's reactive task, and only for events still accessible now.
func (s *Store) RecentMiniAppOrdersSelectionActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]MiniAppOrdersSelectionAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT a.operation_id,op.event_id,(op.result->>'version')::bigint,
 (op.result->>'event_version')::bigint,(op.result->>'catalog_version')::bigint,
 jsonb_array_length(op.result->'selections')=0,a.completed_at
 FROM shuttle_tasks task JOIN shuttle_miniapp_orders_selection_actions a ON a.user_id=task.user_id
 JOIN shuttle_orders_selection_operations op ON op.user_id=a.user_id AND op.operation_id=a.operation_id
 JOIN shuttle_events e ON e.id=op.event_id
 JOIN shuttle_registrations r ON r.event_id=e.id AND r.user_id=a.user_id AND r.state='assigned'
 JOIN shuttle_passes p ON p.registration_id=r.id
 WHERE task.id=$1 AND task.user_id=$2 AND a.completed_at<task.created_at
 AND e.bot_id=$3 AND e.organization_id=$4 AND 'orders'=ANY(e.modules)
 ORDER BY a.completed_at DESC,a.operation_id LIMIT 5`, taskID, scope.UserID, scope.BotID, scope.Organization)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []MiniAppOrdersSelectionAction{}
	for rows.Next() {
		var action MiniAppOrdersSelectionAction
		if err = rows.Scan(
			&action.OperationID,
			&action.EventID,
			&action.DraftVersion,
			&action.EventVersion,
			&action.CatalogVersion,
			&action.Cleared,
			&action.CompletedAt,
		); err != nil {
			return nil, err
		}
		action.CompletedAt = action.CompletedAt.UTC()
		result = append(result, action)
	}
	return result, rows.Err()
}
