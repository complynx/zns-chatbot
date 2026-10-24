package db

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/google/uuid"
)

// KeyboardState is read under the application's short per-conversation send gate.
// No database lock is held during Telegram I/O.
type KeyboardState struct {
	ActiveTaskID        uuid.UUID
	Run                 json.RawMessage
	KeyboardOwnerTaskID uuid.UUID
}

func (s *Store) KeyboardState(ctx context.Context, taskID uuid.UUID) (KeyboardState, error) {
	var state KeyboardState
	err := s.pool.QueryRow(ctx, `SELECT coalesce(c.active_task_id,'00000000-0000-0000-0000-000000000000'::uuid),
 coalesce(a.snapshot->'run','null'::jsonb),coalesce((
 SELECT o.task_id FROM shuttle_tasks newer JOIN shuttle_outbox o ON o.task_id=newer.id
 WHERE newer.conversation_id=t.conversation_id
 AND o.state='delivered' AND coalesce(o.receipt->>'reply_keyboard_hash','')<>''
 AND coalesce(o.receipt->>'reply_keyboard_sent_at','')<>''
 AND o.payload#>'{reply_markup,keyboard}' IS NOT NULL
 ORDER BY (o.receipt->>'reply_keyboard_sent_at')::timestamptz DESC,o.id DESC LIMIT 1),
 '00000000-0000-0000-0000-000000000000'::uuid)
 FROM shuttle_tasks t JOIN shuttle_conversations c ON c.id=t.conversation_id
 LEFT JOIN shuttle_tasks a ON a.id=c.active_task_id WHERE t.id=$1`, taskID).
		Scan(&state.ActiveTaskID, &state.Run, &state.KeyboardOwnerTaskID)
	return state, err
}

// DeliveredPromptContext returns only metadata from an actually delivered message
// in the input task's own conversation. Unknown replies retain their opaque reference.
func (s *Store) DeliveredPromptContext(
	ctx context.Context,
	taskID uuid.UUID,
	messageID int64,
) (json.RawMessage, error) {
	var raw json.RawMessage
	err := s.pool.QueryRow(ctx, "SELECT o.payload->'_loom_context'\n FROM shuttle_tasks input JOIN shuttle" +
"_tasks source ON source.conversation_id=input.conversation_id AND source" +
".user_id=input.user_id\n JOIN shuttle_outbox o ON o.task_id=source.id\n WH" +
"ERE input.id=$1 AND o.state='delivered' AND o.receipt->>'message_id'=$2\n" +
" AND o.payload->'_loom_context' IS NOT NULL\n ORDER BY o.id LIMIT 1", taskID, strconv.FormatInt(messageID, 10)).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return raw, err
}
