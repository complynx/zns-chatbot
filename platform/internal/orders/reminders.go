package orders

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

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
	var pending []delivery.Registration
	for _, order := range due {
		if err = enqueueNotification(
			ctx,
			tx,
			s.Delivery.BotID,
			&pending,
			order.Owner,
			"reminder",
			order,
			nil,
		); err != nil {
			return 0, err
		}
	}
	if err = delivery.RegisterBatch(ctx, tx, s.Delivery.BotID, pending); err != nil {
		return 0, err
	}
	return len(due), tx.Commit(ctx)
}
