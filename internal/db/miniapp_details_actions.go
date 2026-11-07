package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

type MiniAppDetailsAction struct {
	OperationID uuid.UUID                  `json:"operation_id"`
	Field       registrationdetails.Field  `json:"field"`
	Status      registrationdetails.Status `json:"status"`
	CompletedAt time.Time                  `json:"completed_at"`
}

// RecentMiniAppDetailsActions returns bounded, redacted manual actions before
// this owner's user-triggered task. Receipt presence/version are historical.
func (s *Store) RecentMiniAppDetailsActions(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]MiniAppDetailsAction, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT a.operation_id,a.field,(op.result->>'version')::bigint,
(op.result->>'legal_name_present')::boolean,(op.result->>'passport_number_present')::boolean,a.completed_at
FROM shuttle_tasks current_task JOIN shuttle_miniapp_details_actions a ON a.user_id=current_task.user_id
JOIN shuttle_registration_details_operations op ON op.user_id=a.user_id AND op.operation_id=a.operation_id
WHERE current_task.id=$1 AND current_task.user_id=$2 AND a.completed_at<current_task.created_at
ORDER BY a.completed_at DESC,a.operation_id LIMIT 5`, taskID, scope.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	actions := make([]MiniAppDetailsAction, 0)
	for rows.Next() {
		var action MiniAppDetailsAction
		if err = rows.Scan(
			&action.OperationID,
			&action.Field,
			&action.Status.Version,
			&action.Status.LegalNamePresent,
			&action.Status.PassportNumberPresent,
			&action.CompletedAt,
		); err != nil {
			return nil, err
		}
		action.CompletedAt = action.CompletedAt.UTC()
		actions = append(actions, action)
	}
	return actions, rows.Err()
}
