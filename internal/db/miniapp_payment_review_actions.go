package db

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/payments"
)

type MiniAppPaymentReviewAction struct {
	OperationID string    `json:"operation_id"`
	EventID     uuid.UUID `json:"event_id"`
	EvidenceID  uuid.UUID `json:"evidence_id"`
	WorkspaceID uuid.UUID `json:"workspace_id"`
	Revision    int64     `json:"revision"`
	Kind        string    `json:"kind"`
	Outcome     string    `json:"outcome"`
	CompletedAt time.Time `json:"completed_at"`
}

// HasMiniAppPaymentReviewActions avoids an extra IAM role check when this task
// has no native review history. It exposes only this authenticated actor's history bit.
func (s *Store) HasMiniAppPaymentReviewActions(
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
 JOIN shuttle_payment_review_calls c ON c.actor_id=task.user_id
 WHERE task.id=$1 AND task.user_id=$2 AND c.source='miniapp'
 AND c.completed_at<task.created_at)`, taskID, scope.UserID).Scan(&found)
	return found, err
}

// RecentMiniAppPaymentReviewActions returns bounded immutable receipt metadata,
// filtered by current event authority. It discloses no money, reasons or proof data.
func (s *Store) RecentMiniAppPaymentReviewActions(
	ctx context.Context,
	scope PaymentScope,
	taskID uuid.UUID,
) ([]MiniAppPaymentReviewAction, error) {
	if !paymentScopeValid(scope, "read") || taskID == uuid.Nil {
		return nil, payments.ErrInvalid
	}
	tx, err := s.miniAppReviewHistoryTx(ctx, scope)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	rows, err := tx.Query(
		ctx,
		`SELECT c.operation_id,w.event_id,w.evidence_id,w.id,w.revision,c.kind,
 CASE WHEN c.kind='commit' THEN c.result#>>'{review,evidence,state}' ELSE c.result->>'state' END,c.completed_at
 FROM shuttle_tasks task JOIN shuttle_payment_review_calls c ON c.actor_id=task.user_id
 JOIN shuttle_payment_review_workspaces w ON w.id=c.workspace_id AND w.actor_id=c.actor_id
 JOIN shuttle_events e ON e.id=w.event_id
 WHERE task.id=$1 AND task.user_id=$2 AND c.source='miniapp' AND c.completed_at<task.created_at
 AND e.bot_id=$3 AND e.organization_id=$4 AND 'registrations'=ANY(e.modules)
 AND ($5 OR EXISTS(SELECT 1 FROM shuttle_event_authorities a WHERE a.event_id=e.id
 AND a.user_id=c.actor_id AND a.role IN ('ambassador','administrator')))
 ORDER BY c.completed_at DESC,c.operation_id DESC LIMIT 5`,
		taskID,
		scope.UserID,
		scope.BotID,
		scope.Organization,
		scope.GlobalAdmin,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []MiniAppPaymentReviewAction{}
	for rows.Next() {
		var action MiniAppPaymentReviewAction
		if err = rows.Scan(
			&action.OperationID,
			&action.EventID,
			&action.EvidenceID,
			&action.WorkspaceID,
			&action.Revision,
			&action.Kind,
			&action.Outcome,
			&action.CompletedAt,
		); err != nil {
			return nil, err
		}
		action.CompletedAt = action.CompletedAt.UTC()
		result = append(result, action)
	}
	return result, rows.Err()
}

// miniAppReviewHistoryTx validates the current actor before reading native review history.
func (s *Store) miniAppReviewHistoryTx(ctx context.Context, scope PaymentScope) (pgx.Tx, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if err = lockProfileOwner(
		ctx,
		tx,
		ProfileScope{
			BotID:        scope.BotID,
			Organization: scope.Organization,
			Subject:      scope.Subject,
			UserID:       scope.UserID,
		},
		false,
	); err != nil {
		rollbackLegacyArchive(ctx, tx)
		return nil, err
	}
	return tx, nil
}
