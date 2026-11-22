package db

import (
	"context"
	"strconv"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PassEntryPrompt verifies the owner, exact delivered message and offered action.
func (s *Store) PassEntryPrompt(
	ctx context.Context, scope ProfileScope, taskID, promptID uuid.UUID, messageID int64, callback string,
) ([]byte, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	var raw []byte
	var state string
	var receiptMessageID *string
	err = tx.QueryRow(ctx, `
		SELECT prior.snapshot->'pass_entry',o.state,o.receipt->>'message_id'
		FROM shuttle_tasks current_task
		JOIN shuttle_tasks prior ON prior.user_id=current_task.user_id
			AND prior.conversation_id=current_task.conversation_id AND prior.seq<current_task.seq
		JOIN shuttle_outbox o ON o.task_id=prior.id
		WHERE current_task.id=$1 AND current_task.user_id=$2 AND prior.id=$3
			AND prior.route='structured' AND prior.state='completed'
			AND prior.request->>'structured_command'='/passes'
			AND prior.created_at+interval '2 hours'>current_task.created_at
			AND prior.snapshot->'pass_entry' IS NOT NULL
			AND o.state IN ('pending','retryable','send_inflight','delivered')
			AND o.payload#>'{reply_markup,inline_keyboard}' @>
				jsonb_build_array(jsonb_build_array(jsonb_build_object('callback_data',$4::text)))
		LIMIT 1`, taskID, scope.UserID, promptID, callback).Scan(&raw, &state, &receiptMessageID)
	if err != nil || len(raw) == 0 {
		if err == nil {
			err = pgx.ErrNoRows
		}
		return nil, err
	}
	// Telegram can expose the message just before the send receipt is committed.
	if state != deliveryDelivered {
		return nil, ErrRoutingPending
	}
	if receiptMessageID == nil || *receiptMessageID != strconv.FormatInt(messageID, 10) {
		return nil, pgx.ErrNoRows
	}
	return raw, nil
}
