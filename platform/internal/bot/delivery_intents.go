package bot

import (
	"context"
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

// enqueueBotIntent binds one immutable effect to the shared transport lane.
// Retrying a terminal effect returns its receipt; it never revives that effect.
func (b *Bot) enqueueBotIntent(
	ctx context.Context,
	owner string,
	chat int64,
	operation, effect string,
	reference botdelivery.Reference,
	phase string,
) (botdelivery.Observation, error) {
	return b.Host.EnqueueBotDelivery(
		ctx,
		botdelivery.EnqueueRequest{
			Owner:     owner,
			Chat:      chat,
			Operation: operation,
			Effect:    effect,
			Reference: reference,
			Phase:     phase,
		},
	)
}

// Domain/source locks precede the intent row and all shared delivery locks.

func (b *Bot) beginBotIntent(
	ctx context.Context,
	observed botdelivery.Intent,
	rendered botRenderedDelivery,
) (botdelivery.Intent, bool, error) {
	prepared, err := prepareBotAttempt(observed, rendered)
	if err != nil {
		return observed, false, err
	}
	result, err := b.Host.BeginBotDelivery(
		ctx,
		botdelivery.BeginRequest{
			Observed:     observed,
			Prepared:     &prepared,
			Pass:         rendered.Receipt.Pass,
			Target:       rendered.Payload.MessageID,
			ExportEvents: rendered.ExportEvents,
		},
	)
	return result.Intent, result.Ready, err
}

func (b *Bot) finishBotIntent(
	ctx context.Context,
	attempt botdelivery.Intent,
	outcome delivery.Outcome,
	receipt botdelivery.Continuation,
	fallback bool,
) error {
	return (botdelivery.Service{DB: b.DB, Delivery: b.Delivery}).Complete(
		ctx,
		botdelivery.CompletionRequest{Attempt: attempt, Outcome: outcome, Receipt: receipt, Fallback: fallback},
	)
}
func (b *Bot) postponeBotIntent(ctx context.Context, observed botdelivery.Intent, terminal bool) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := botdelivery.Read(ctx, tx, observed.BotID, observed.QueueReference(), true)
	if err != nil {
		return err
	}
	if current.State != delivery.Deferred || current.Attempt != observed.Attempt {
		return nil
	}
	state, reason := delivery.Deferred, "source_unavailable"
	var deadline time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()+$1::bigint*interval '1 microsecond'", b.Delivery.Fallback.Microseconds()).
		Scan(&deadline); err != nil {
		return core.DatabaseOperationError(err)
	}
	if terminal {
		state, reason = delivery.Cancelled, "source_unavailable"
	}
	if err = delivery.Project(ctx, tx, current.BotID, current.QueueReference(), state, deadline); err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.delivery_intents SET state=$4,reason=$5,not_before=$6 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID,
		current.Operation,
		current.Effect,
		state,
		reason,
		deadline,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

// RecoverBotIntents runs only after runtime proves exclusive replacement
// ownership. An admitted wire without a known receipt must retain its lane head.
func (b *Bot) RecoverBotIntents(ctx context.Context) error {
	if err := b.Delivery.Validate(); err != nil {
		return err
	}
	for {
		tx, err := b.DB.Begin(ctx)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
		var operation, effect string
		err = tx.QueryRow(ctx, `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE bot_id=$1 AND state='sending' ORDER BY operation_key,effect_key FOR UPDATE SKIP LOCKED LIMIT 1`, b.Delivery.BotID).Scan(&operation, &effect)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			return nil
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return core.DatabaseOperationError(err)
		}
		ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
		err = delivery.Project(ctx, tx, b.Delivery.BotID, ref, delivery.Uncertain, time.Time{})
		if err == nil {
			_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET state='unknown',reason='delivery_interrupted'
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`, b.Delivery.BotID, operation, effect)
			err = core.DatabaseOperationError(err)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
}
