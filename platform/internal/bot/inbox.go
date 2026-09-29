package bot

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Persist the whole batch before processing any event. Only this durable cursor
// may be sent back to Telegram as acknowledgement of received updates.
func (b *Bot) saveBatch(ctx context.Context, offset int64, updates []telegram.Update) (int64, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return offset, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	next := offset
	updates = slices.Clone(updates)
	slices.SortFunc(updates, func(a, b telegram.Update) int { return cmp.Compare(a.ID, b.ID) })
	for _, update := range updates {
		if update.ID < 0 || update.ID == math.MaxInt64 {
			return offset, errors.New("invalid Telegram update ID")
		}
		if update.ID < offset {
			continue
		}
		if err = b.saveRegistrationIngress(ctx, tx, update); err != nil {
			return offset, err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO bot.telegram_inbox(update_id,payload)
VALUES($1,$2) ON CONFLICT DO NOTHING`, update.ID, update); err != nil {
			return offset, err
		}
		next = max(next, update.ID+1)
	}
	if _, err = tx.Exec(ctx, `UPDATE bot.cursors SET value=$1 WHERE name='telegram_received'`, next); err != nil {
		return offset, err
	}
	if err = tx.Commit(ctx); err != nil {
		return offset, err
	}
	return next, nil
}

// A single poller owns the inbox. Retry failures through existing idempotency
// checks; a durably redacted final plan is terminal and can be acknowledged.
func (b *Bot) drainInbox(ctx context.Context) error {
	for {
		var update telegram.Update
		err := b.DB.QueryRow(ctx, `SELECT payload FROM bot.telegram_inbox ORDER BY update_id LIMIT 1`).Scan(&update)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err = b.Handle(ctx, update); err != nil {
			if !errors.Is(err, errHistoryPlanTerminal) && !errors.Is(err, errPassPlanTerminal) {
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
		if err = b.finishUpdate(ctx, update.ID); err != nil {
			return err
		}
	}
}

func (b *Bot) finishUpdate(ctx context.Context, id int64) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(
		ctx,
		`UPDATE bot.cursors SET value=GREATEST(value,$1) WHERE name='telegram'`,
		id+1,
	); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM bot.telegram_inbox WHERE update_id=$1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
