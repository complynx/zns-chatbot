package orders

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

const DefaultReminderAfter = 48 * time.Hour
const reminderBatchSize = 100

// QueueDueReminders claims eligible orders and writes their notices in one transaction.
func (s Service) QueueDueReminders(ctx context.Context, after time.Duration) (int, error) {
	if after < time.Second {
		return 0, errors.New("reminder interval must be at least one second")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or a reported scan error.
	rows, err := tx.Query(ctx, `WITH due AS (
 SELECT id AS candidate_id FROM core.orders WHERE state IN ('unpaid','cash')
 AND (choice->>'total')::numeric>0 AND reminder_claimed_at IS NULL
 AND created_at<=clock_timestamp()-($1*interval '1 second')
 ORDER BY created_at,id LIMIT $2 FOR UPDATE SKIP LOCKED
 ) UPDATE core.orders SET reminder_claimed_at=clock_timestamp() FROM due WHERE id=due.candidate_id RETURNING `+columns,
		int64(after/time.Second), reminderBatchSize)
	if err != nil {
		return 0, err
	}
	due, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Order, error) { return scan(row) })
	if err != nil {
		return 0, err
	}
	for _, order := range due {
		if err = enqueueNotification(ctx, tx, order.Owner, "reminder", order, nil); err != nil {
			return 0, err
		}
	}
	return len(due), tx.Commit(ctx)
}

// ClaimReminder preserves Python's one delivery attempt, including ambiguous failures.
func (s Service) ClaimReminder(ctx context.Context, id int64) (bool, error) {
	if id <= 0 {
		return false, problem(http.StatusBadRequest, "invalid_delivery")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup after commit or a reported claim error.
	var eligible bool
	err = tx.QueryRow(ctx, `SELECT o.state IN ('unpaid','cash') AND (o.choice->>'total')::numeric>0
 FROM core.orders o JOIN core.order_notifications n ON n.order_id=o.id
 WHERE n.id=$1 AND n.payload->>'kind'='reminder' AND n.delivered_at IS NULL AND n.attempted_at IS NULL
 FOR UPDATE OF o`, id).Scan(&eligible)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	result, err := tx.Exec(ctx, `UPDATE core.order_notifications SET attempted_at=clock_timestamp(),
 delivered_at=CASE WHEN $2 THEN NULL ELSE clock_timestamp() END
 WHERE id=$1 AND attempted_at IS NULL AND delivered_at IS NULL`, id, eligible)
	if err != nil {
		return false, err
	}
	return eligible && result.RowsAffected() == 1, tx.Commit(ctx)
}
