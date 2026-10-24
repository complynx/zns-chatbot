package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type MiniAppOrderEvidenceAction struct {
	OrderKind   OrderKind `json:"order_kind"`
	OperationID uuid.UUID `json:"operation_id"`
	EventID     uuid.UUID `json:"event_id"`
	OrderID     uuid.UUID `json:"order_id"`
	EvidenceID  uuid.UUID `json:"evidence_id"`
	Outcome     string    `json:"outcome"`
	CompletedAt time.Time `json:"completed_at"`
}

// RecentMiniAppOrderEvidenceActions exposes original receipts accepted through the form,
// bounded by the owner's task time and current assigned-pass/event access.
func (s *Store) RecentMiniAppOrderEvidenceActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]MiniAppOrderEvidenceAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, "SELECT a.operation_id,op.event_id,(op.result->>'order_id')::uuid,\n (op.r" +
"esult->>'id')::uuid,op.result->>'state',a.completed_at,ev.order_kind\n FR" +
"OM shuttle_tasks task JOIN shuttle_miniapp_order_evidence_actions a ON a" +
".user_id=task.user_id\n JOIN shuttle_order_evidence_operations op ON op.u" +
"ser_id=a.user_id AND op.operation_id=a.operation_id AND op.kind=a.kind\n " +
"JOIN shuttle_order_evidence ev ON ev.id=(op.result->>'id')::uuid AND ev." +
"event_id=op.event_id AND ev.user_id=op.user_id\n JOIN shuttle_events e ON" +
" e.id=op.event_id\n JOIN shuttle_registrations r ON r.event_id=e.id AND r" +
".user_id=a.user_id AND r.state='assigned'\n JOIN shuttle_passes p ON p.re" +
"gistration_id=r.id\n WHERE task.id=$1 AND task.user_id=$2 AND a.completed" +
"_at<task.created_at\n AND e.bot_id=$3 AND e.organization_id=$4 AND (CASE " +
"ev.order_kind WHEN 'service' THEN 'orders' WHEN 'meal' THEN 'meals' END)" +
"=ANY(e.modules)\n ORDER BY a.completed_at DESC,a.operation_id DESC LIMIT " +
"5", taskID, scope.UserID, scope.BotID, scope.Organization)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MiniAppOrderEvidenceAction, error) {
		var action MiniAppOrderEvidenceAction
		scanErr := row.Scan(
			&action.OperationID,
			&action.EventID,
			&action.OrderID,
			&action.EvidenceID,
			&action.Outcome,
			&action.CompletedAt,
			&action.OrderKind,
		)
		action.CompletedAt = action.CompletedAt.UTC()
		return action, scanErr
	})
}
