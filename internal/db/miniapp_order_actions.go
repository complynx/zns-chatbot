package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

type MiniAppOrderAction struct {
	OperationID   uuid.UUID          `json:"operation_id"`
	EventID       uuid.UUID          `json:"event_id"`
	Kind          OrderOperationKind `json:"kind"`
	PreparationID *uuid.UUID         `json:"proposal_id,omitempty"`
	OrderID       *uuid.UUID         `json:"order_id,omitempty"`
	RecordedAt    time.Time          `json:"recorded_at"`
}

// RecentMiniAppOrderActions projects existing receipts, not a second workflow
// log. Only this task owner's currently accessible events and earlier receipt
// timestamps qualify. Metadata is historical, not current state or consent.
func (s *Store) RecentMiniAppOrderActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]MiniAppOrderAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT op.operation_id,op.event_id,op.kind,op.preparation_id,op.order_id,op.created_at
 FROM shuttle_tasks task JOIN shuttle_order_operations op ON op.user_id=task.user_id
 JOIN shuttle_events e ON e.id=op.event_id
 JOIN shuttle_registrations r ON r.event_id=e.id AND r.user_id=op.user_id AND r.state='assigned'
 JOIN shuttle_passes p ON p.registration_id=r.id
 WHERE task.id=$1 AND task.user_id=$2 AND op.source='miniapp' AND op.created_at<task.created_at
 AND e.bot_id=$3 AND e.organization_id=$4 AND 'orders'=ANY(e.modules)
 ORDER BY op.created_at DESC,op.operation_id DESC,op.kind LIMIT 5`, taskID, scope.UserID, scope.BotID, scope.Organization)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []MiniAppOrderAction{}
	for rows.Next() {
		var action MiniAppOrderAction
		var preparation, order pgtype.UUID
		if err = rows.Scan(
			&action.OperationID,
			&action.EventID,
			&action.Kind,
			&preparation,
			&order,
			&action.RecordedAt,
		); err != nil {
			return nil, err
		}
		if preparation.Valid {
			id := uuid.UUID(preparation.Bytes)
			action.PreparationID = &id
		}
		if order.Valid {
			id := uuid.UUID(order.Bytes)
			action.OrderID = &id
		}
		action.RecordedAt = action.RecordedAt.UTC()
		result = append(result, action)
	}
	return result, rows.Err()
}
