package adminmessage

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
)

// Claim is service-only. A lease lasts two minutes; workers must finish their send
// before it expires. Attempt fences stale completions after a crash/reclaim.
func (s Service) Claim(ctx context.Context) (Delivery, bool, error) {
	var result Delivery
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Revocation/cancellation also retires crashed work once its lease expires.
	_, err = tx.Exec(ctx, `UPDATE core.admin_message_deliveries d SET state='cancelled'
 FROM core.admin_messages m WHERE m.id=d.message_id
 AND (d.state='pending' OR (d.state='sending' AND d.available_at<=clock_timestamp()))
 AND (m.state='cancelled' OR NOT EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=m.actor))`)
	if err != nil {
		return result, false, err
	}
	rows, err := tx.Query(
		ctx,
		`SELECT d.id,d.message_id,d.destination,COALESCE(d.content,m.request->'content'),d.state,d.attempt
 FROM core.admin_message_deliveries d JOIN core.admin_messages m ON m.id=d.message_id
 JOIN core.pass_booking_admins a ON a.owner=m.actor
 WHERE m.state='queued' AND d.state IN ('pending','sending') AND d.available_at<=clock_timestamp()
 ORDER BY d.available_at,d.id LIMIT 1 FOR UPDATE OF d,m SKIP LOCKED FOR SHARE OF a`,
	)
	if err != nil {
		return result, false, err
	}
	deliveries, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Delivery, error) {
		var delivery Delivery
		scanErr := row.Scan(
			&delivery.ID,
			&delivery.MessageID,
			&delivery.Destination,
			&delivery.Content,
			&delivery.State,
			&delivery.Attempt,
		)
		return delivery, scanErr
	})
	if err != nil {
		return result, false, err
	}
	if len(deliveries) == 0 {
		return result, false, tx.Commit(ctx)
	}
	result = deliveries[0]
	result.Attempt++
	result.State = "sending"
	_, err = tx.Exec(ctx, `UPDATE core.admin_message_deliveries SET state='sending',attempt=$2,
 available_at=clock_timestamp()+interval '2 minutes' WHERE id=$1`, result.ID, result.Attempt)
	if err != nil {
		return result, false, err
	}
	return result, true, tx.Commit(ctx)
}

// Complete is service-only. Without an upstream cooldown, retries wait30seconds.
// Telegram does not offer an idempotency key: a crash after send but before Complete
// can duplicate delivery. Do not describe this contract as exactly-once.
func (s Service) Complete(ctx context.Context, id, attempt, telegramMessageID int64, failure string, retry bool) error {
	return s.CompleteDelivery(
		ctx,
		Completion{ID: id, Attempt: attempt, MessageID: telegramMessageID, Failure: failure, Retry: retry},
	)
}

// CompleteDelivery persists the maximum of the minimum backoff and Telegram's
// structured cooldown. Invalid durations fail closed before scheduling work.
func (s Service) CompleteDelivery(ctx context.Context, result Completion) error {
	id, attempt, telegramMessageID := result.ID, result.Attempt, result.MessageID
	failure, retry := result.Failure, result.Retry
	if result.RetryAfter < 0 || result.RetryAfter > MaxRetryAfterSeconds || (!retry && result.RetryAfter != 0) {
		return invalid()
	}
	const minimumRetrySeconds int64 = 30
	retrySeconds := max(minimumRetrySeconds, result.RetryAfter)
	if id <= 0 || attempt <= 0 || len(failure) > maxFailureBytes || telegramMessageID < 0 ||
		(telegramMessageID > 0 && (failure != "" || retry)) || (telegramMessageID == 0 && failure == "") {
		return invalid()
	}
	state := "sent"
	if failure != "" {
		state = "failed"
		if retry {
			state = statePending
		}
	}
	tag, err := s.DB.Exec(ctx, `UPDATE core.admin_message_deliveries SET state=$3,telegram_message_id=$4,
 failure=$5,available_at=clock_timestamp()+make_interval(secs => $6) WHERE id=$1 AND attempt=$2 AND state='sending'`,
		id, attempt, state, telegramMessageID, failure, retrySeconds)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return problem(http.StatusConflict, "admin_message_stale_attempt")
	}
	return nil
}
