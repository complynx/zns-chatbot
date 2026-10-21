package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/orders"
)

type MiniAppOrderReviewAction struct {
	OrderKind   OrderKind `json:"order_kind"`
	OperationID uuid.UUID `json:"operation_id"`
	EventID     uuid.UUID `json:"event_id"`
	EvidenceID  uuid.UUID `json:"evidence_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Revision    int64     `json:"revision"`
	Kind        string    `json:"kind"`
	Outcome     string    `json:"outcome"`
	CompletedAt time.Time `json:"completed_at"`
}

// HasMiniAppOrderReviewActions avoids an extra IAM role check when this task
// has no native review history. It exposes only this authenticated actor's history bit.
func (s *Store) HasMiniAppOrderReviewActions(
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
	var found bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_tasks task
 JOIN shuttle_order_review_calls c ON c.actor_id=task.user_id
 WHERE task.id=$1 AND task.user_id=$2 AND c.source='miniapp'
 AND c.completed_at<task.created_at)`, taskID, scope.UserID).Scan(&found)
	return found, err
}

// RecentMiniAppOrderReviewActions returns bounded immutable receipt metadata,
// filtered by current event authority. It discloses no money, reasons or proof data.
func (s *Store) RecentMiniAppOrderReviewActions(
	ctx context.Context,
	scope PaymentScope,
	taskID uuid.UUID,
) ([]MiniAppOrderReviewAction, error) {
	if !paymentScopeValid(scope, "read") || taskID == uuid.Nil {
		return nil, orders.ErrSelection
	}
	tx, err := s.miniAppReviewHistoryTx(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := tx.Query(ctx, `SELECT c.operation_id,w.event_id,w.evidence_id,w.id,w.revision,c.kind,
 CASE WHEN c.kind='commit' THEN c.result#>>'{evidence,state}' ELSE c.result->>'state' END,c.completed_at,ev.order_kind
 FROM shuttle_tasks task JOIN shuttle_order_review_calls c ON c.actor_id=task.user_id
 JOIN shuttle_order_review_workspaces w ON w.id=c.workspace_id AND w.actor_id=c.actor_id
 JOIN shuttle_order_evidence ev ON ev.id=w.evidence_id AND ev.event_id=w.event_id
 JOIN shuttle_events e ON e.id=w.event_id
 WHERE task.id=$1 AND task.user_id=$2 AND c.source='miniapp' AND c.completed_at<task.created_at
 AND e.bot_id=$3 AND e.organization_id=$4 AND (CASE ev.order_kind WHEN 'service' THEN 'orders' WHEN 'meal' THEN 'meals' END)=ANY(e.modules)
 AND ($5 OR EXISTS(SELECT 1 FROM shuttle_event_authorities a WHERE a.event_id=e.id
 AND a.user_id=c.actor_id AND a.role IN ('ambassador','administrator')))
 ORDER BY c.completed_at DESC,c.operation_id DESC LIMIT 5`, taskID, scope.UserID, scope.BotID, scope.Organization, scope.GlobalAdmin)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (MiniAppOrderReviewAction, error) {
		var action MiniAppOrderReviewAction
		scanErr := row.Scan(
			&action.OperationID,
			&action.EventID,
			&action.EvidenceID,
			&action.WorkspaceID,
			&action.Revision,
			&action.Kind,
			&action.Outcome,
			&action.CompletedAt,
			&action.OrderKind,
		)
		action.CompletedAt = action.CompletedAt.UTC()
		return action, scanErr
	})
}
