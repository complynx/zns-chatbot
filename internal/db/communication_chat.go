package db

import (
	"context"

	"github.com/google/uuid"
)

// ClaimCommunicationChat commits an empty marker before the non-idempotent chat
// creation call. An existing empty marker means its outcome must not be replayed.
func (s *Store) ClaimCommunicationChat(ctx context.Context, id uuid.UUID) (string, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var chat *string
	err = tx.QueryRow(ctx, `SELECT personalization_chat_id FROM shuttle_communication_deliveries
 WHERE id=$1 AND state IN ('pending','retryable') FOR UPDATE`, id).Scan(&chat)
	if err != nil {
		return "", false, err
	}
	if chat != nil {
		return *chat, false, tx.Commit(ctx)
	}
	_, err = tx.Exec(ctx, `UPDATE shuttle_communication_deliveries SET personalization_chat_id='' WHERE id=$1`, id)
	if err != nil {
		return "", false, err
	}
	return "", true, tx.Commit(ctx)
}

// SaveCommunicationChat saves the isolated chat, or releases a definitively
// rejected creation attempt. Unknown outcomes retain the marker and fail safely.
func (s *Store) SaveCommunicationChat(ctx context.Context, id uuid.UUID, chat string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE shuttle_communication_deliveries SET personalization_chat_id=nullif($2,'')
 WHERE id=$1 AND personalization_chat_id='' AND state IN ('pending','retryable')`, id, chat)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrConflict
	}
	return err
}
