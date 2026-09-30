package bot

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"math"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Persist the whole batch before processing any event. Only this durable cursor
// may be sent back to Telegram as acknowledgement of received updates.
func (b *Bot) saveBatch(ctx context.Context, offset int64, updates []telegram.Update) (int64, error) {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return offset, inboxDatabaseError(ctx, err)
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
			return offset, inboxDatabaseError(ctx, err)
		}
		next = max(next, update.ID+1)
	}
	if _, err = tx.Exec(ctx, `UPDATE bot.cursors SET value=$1 WHERE name='telegram_received'`, next); err != nil {
		return offset, inboxDatabaseError(ctx, err)
	}
	if err = tx.Commit(ctx); err != nil {
		return offset, inboxDatabaseError(ctx, err)
	}
	return next, nil
}

// A single poller owns the inbox. SQL failure, cancellation and credit
// configuration leave the update for a fresh runtime; a durably redacted final
// plan is terminal and can be acknowledged. Other handler failures retry the
// same chat head with backoff while unrelated chats and polling continue.
func (b *Bot) drainInbox(ctx context.Context) error {
	return b.drainInboxWith(ctx, b.Handle)
}

// The bounded pass returns so poll can fetch Telegram again with a large backlog.
func (b *Bot) drainInboxWith(ctx context.Context, handle func(context.Context, telegram.Update) error) error {
	for range inboxPassLimit {
		row, found, err := b.claimInboxRow(ctx)
		if err != nil || !found {
			return err
		}
		if err = b.processInboxRow(ctx, row, handle); err != nil {
			return err
		}
	}
	return nil
}

// The inbox row key is authoritative for dispatch and all bookkeeping.
func (b *Bot) processInboxRow(
	ctx context.Context,
	row inboxRow,
	handle func(context.Context, telegram.Update) error,
) error {
	// A readable row with an incompatible payload is not a database failure.
	var update telegram.Update
	if json.Unmarshal(row.payload, &update) != nil {
		return b.quarantineInboxRow(ctx, row, inboxFailureMalformed)
	}
	if update.ID != row.id {
		return b.quarantineInboxRow(ctx, row, inboxFailureMismatch)
	}
	err := handle(ctx, update)
	outcome, deadline := classifyInboxResult(ctx, err, row.failures)
	switch outcome {
	case inboxFinish:
		return b.finishUpdate(ctx, row.id)
	case inboxDefer:
		return b.deferInboxRow(ctx, row, deadline)
	case inboxFail:
		return b.failInboxRow(ctx, row, deadline, err)
	case inboxStop:
	}
	b.releaseInboxLease(ctx, row)
	return err
}

func (b *Bot) finishUpdate(ctx context.Context, id int64) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return inboxDatabaseError(ctx, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(
		ctx,
		`UPDATE bot.cursors SET value=GREATEST(value,$1) WHERE name='telegram'`,
		id+1,
	); err != nil {
		return inboxDatabaseError(ctx, err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM bot.telegram_inbox WHERE update_id=$1`, id); err != nil {
		return inboxDatabaseError(ctx, err)
	}
	return inboxDatabaseError(ctx, tx.Commit(ctx))
}

// The caller knows this failure came from a database operation, not a provider.
func inboxDatabaseError(ctx context.Context, err error) error {
	return core.DatabaseOperationContextError(ctx, err)
}
