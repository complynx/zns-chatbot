package adminmessage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type InputExpiry struct {
	ID       int64  `json:"id"`
	ChatID   int64  `json:"chat_id"`
	Attempt  int64  `json:"attempt"`
	Language string `json:"language"`
}

// ClaimInputExpiry makes timeouts survive restarts. A crash after notification but
// before acknowledgement can duplicate the notice, never a broadcast delivery.
func (s Service) ClaimInputExpiry(ctx context.Context) (InputExpiry, bool, error) {
	var result InputExpiry
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `DELETE FROM core.admin_message_sources WHERE expires_at<=clock_timestamp()`); err != nil {
		return result, false, err
	}
	err = tx.QueryRow(ctx, `SELECT i.id,i.chat_id,i.notice_attempt+1,u.language FROM core.admin_message_inputs i JOIN core.users u ON u.id=i.actor JOIN core.pass_booking_admins a ON a.owner=i.actor
 WHERE i.state IN ('pending','expired') AND i.expires_at<=clock_timestamp() AND NOT i.notice_done AND i.notice_available_at<=clock_timestamp() ORDER BY i.id LIMIT 1 FOR UPDATE OF i SKIP LOCKED FOR SHARE OF a`).
		Scan(&result.ID, &result.ChatID, &result.Attempt, &result.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, false, tx.Commit(ctx)
	}
	if err != nil {
		return result, false, err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET state='expired',notice_attempt=$2,notice_available_at=clock_timestamp()+interval '2 minutes' WHERE id=$1`,
		result.ID,
		result.Attempt,
	)
	if err != nil {
		return result, false, err
	}
	return result, true, tx.Commit(ctx)
}

func (s Service) CompleteInputExpiry(ctx context.Context, id, attempt int64) error {
	tag, err := s.DB.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET notice_done=true WHERE id=$1 AND notice_attempt=$2 AND state='expired'`,
		id,
		attempt,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return rejected("admin_message_stale_attempt")
	}
	return nil
}
