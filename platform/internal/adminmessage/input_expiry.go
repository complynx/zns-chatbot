package adminmessage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type InputExpiry struct {
	ID       int64  `json:"id"`
	Actor    string `json:"actor"`
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
	err = tx.QueryRow(ctx, `SELECT i.id,i.actor,i.chat_id,i.notice_attempt+1,u.language FROM core.admin_message_inputs i JOIN core.users u ON u.id=i.actor JOIN core.pass_booking_admins a ON a.owner=i.actor
 WHERE i.state IN ('pending','expired') AND i.expires_at<=clock_timestamp() AND NOT i.notice_done AND i.notice_available_at<=clock_timestamp() ORDER BY i.id LIMIT 1 FOR UPDATE OF i SKIP LOCKED FOR SHARE OF a`).
		Scan(&result.ID, &result.Actor, &result.ChatID, &result.Attempt, &result.Language)
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
		`UPDATE core.admin_message_inputs i SET notice_done=true WHERE id=$1 AND notice_attempt=$2 AND state='expired' AND expires_at<=clock_timestamp() AND EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=i.actor)`,
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

// CurrentInputExpiry rechecks a durable notice source without claiming another generation.
func (s Service) CurrentInputExpiry(ctx context.Context, id int64) (InputExpiry, bool, error) {
	var result InputExpiry
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var actor string
	err = tx.QueryRow(ctx, `SELECT i.actor FROM core.admin_message_inputs i JOIN core.pass_booking_admins a ON a.owner=i.actor WHERE i.id=$1`, id).
		Scan(&actor)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	if _, err = guardInput(ctx, tx, actor, id); err != nil {
		if _, revoked := errors.AsType[*revokedSourceError](err); revoked {
			return result, false, tx.Commit(ctx)
		}
		return result, false, err
	}
	err = tx.QueryRow(ctx, `SELECT i.id,i.actor,i.chat_id,i.notice_attempt,u.language
 FROM core.admin_message_inputs i JOIN core.users u ON u.id=i.actor
 WHERE i.id=$1 AND i.state IN ('pending','expired') AND i.expires_at<=clock_timestamp()
 AND NOT i.notice_done FOR UPDATE OF i`, id).Scan(&result.ID, &result.Actor, &result.ChatID, &result.Attempt, &result.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, false, nil
	}
	if err != nil {
		return result, false, err
	}
	return result, true, tx.Commit(ctx)
}

// CompleteInputExpiryInTx validates the exact durable actor/chat/attempt before
// acknowledging a known delivery; repeating the same acknowledgement is safe.
func (s Service) CompleteInputExpiryInTx(ctx context.Context, tx pgx.Tx, actor string, chat, id int64) error {
	if _, err := guardInput(ctx, tx, actor, id); err != nil {
		return err
	}
	if err := authorize(ctx, tx, actor); err != nil {
		return err
	}
	tag, err := tx.Exec(
		ctx,
		`UPDATE core.admin_message_inputs SET notice_done=true WHERE id=$1 AND actor=$2 AND chat_id=$3 AND state='expired' AND expires_at<=clock_timestamp()`,
		id,
		actor,
		chat,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return rejected("admin_message_stale_attempt")
	}
	return nil
}
