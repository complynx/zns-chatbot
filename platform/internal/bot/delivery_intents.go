package bot

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

const botEditTargetMissing = "edit_target_missing"

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
	result, err := b.Host.BeginBotDelivery(
		ctx,
		botdelivery.BeginRequest{
			Wire:         rendered.Wire,
			Observed:     observed,
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
	if attempt.BotID != b.Delivery.BotID {
		return botdelivery.ErrBinding
	}
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	current, err := botdelivery.Read(ctx, tx, attempt.BotID, attempt.QueueReference(), true)
	if err != nil {
		return err
	}
	if current.Attempt != attempt.Attempt {
		return botdelivery.ErrBinding
	}
	if current.State == delivery.Rejected || current.State == delivery.Cancelled {
		return recordBotTerminalReceipt(ctx, tx, current, outcome, fallback)
	}
	outcome, deadline, err := b.finishBotTransportOutcome(ctx, tx, current, outcome, fallback)
	if err != nil {
		return err
	}
	phase, target := current.Phase, current.Target
	if fallback {
		phase, target = botPhaseSend, 0
	}
	if deadline.IsZero() {
		if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&deadline); err != nil {
			return core.DatabaseOperationError(err)
		}
	}
	if current.Reference.Family == botFamilyPasses && current.Receipt.Pass != nil {
		// Preserve the canonical admission binding across fallback and success.
		receipt.Pass = current.Receipt.Pass
	}
	raw, err := json.Marshal(receipt)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE bot.delivery_intents SET state=$4,message_id=$5,reason=$6,not_before=$7,receipt=$8,phase=$9,target_message_id=$10
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID,
		current.Operation,
		current.Effect,
		outcome.Kind,
		outcome.MessageID,
		outcome.Reason,
		deadline,
		raw,
		phase,
		target,
	)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

// The caller holds the owner row lock and has checked the exact admitted attempt.
func (b *Bot) finishBotTransportOutcome(
	ctx context.Context,
	tx pgx.Tx,
	current botdelivery.Intent,
	outcome delivery.Outcome,
	fallback bool,
) (delivery.Outcome, time.Time, error) {
	known := knownBotTransportOutcome(outcome, fallback)
	lateKnown := false
	if current.State == delivery.Deferred && known {
		err := tx.QueryRow(ctx, `SELECT COALESCE(last_uncertain_attempt=$4,false)
 AND last_confirmed_attempt IS DISTINCT FROM $4 FROM bot.delivery_intents
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
			current.BotID, current.Operation, current.Effect, current.Attempt).Scan(&lateKnown)
		if err != nil {
			return delivery.Outcome{}, time.Time{}, core.DatabaseOperationError(err)
		}
	}
	if !lateKnown && current.State != delivery.Sending && current.State != delivery.Uncertain {
		return delivery.Outcome{}, time.Time{}, botdelivery.ErrBinding
	}
	if fallback && (current.Phase != botPhaseEdit || outcome.Kind != delivery.Deferred) {
		return delivery.Outcome{}, time.Time{}, botdelivery.ErrBinding
	}
	var deadline time.Time
	var err error
	if lateKnown && outcome.Kind == delivery.Succeeded {
		outcome, deadline, err = delivery.FinishUncertainSuccess(ctx, tx, b.Delivery, current.QueueReference(), outcome)
	} else {
		outcome, deadline, err = delivery.Finish(ctx, tx, b.Delivery, current.QueueReference(), outcome)
	}
	if err != nil {
		return delivery.Outcome{}, time.Time{}, err
	}
	if known {
		_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET last_confirmed_attempt=$4
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND attempt=$4`,
			current.BotID, current.Operation, current.Effect, current.Attempt)
		if err != nil {
			return delivery.Outcome{}, time.Time{}, core.DatabaseOperationError(err)
		}
	}
	return b.retryBotTransport(ctx, tx, current, outcome, deadline)
}

func knownBotTransportOutcome(outcome delivery.Outcome, fallback bool) bool {
	if !outcome.Valid() {
		return false
	}
	if fallback {
		return outcome.Kind == delivery.Deferred && outcome.Reason == botEditTargetMissing
	}
	switch outcome.Kind {
	case delivery.Succeeded:
		return true
	case delivery.Deferred:
		return outcome.Reason == "telegram_rate_limit"
	case delivery.Rejected:
		return outcome.Reason == "telegram_recipient_rejected"
	case delivery.Paused:
		return outcome.Reason == "telegram_service_rejected"
	case delivery.Parked:
		return outcome.Reason == "telegram_invalid_cooldown"
	case delivery.Sending, delivery.Cancelled, delivery.Uncertain:
		return false
	default:
		return false
	}
}

// Late confirmed responses preserve evidence without reviving terminal work.
func recordBotTerminalReceipt(
	ctx context.Context,
	tx pgx.Tx,
	current botdelivery.Intent,
	outcome delivery.Outcome,
	fallback bool,
) error {
	if outcome.Kind != delivery.Succeeded && knownBotTransportOutcome(outcome, fallback) {
		if fallback && current.Phase != botPhaseEdit {
			return botdelivery.ErrBinding
		}
		updated, err := tx.Exec(ctx, `UPDATE bot.delivery_intents SET last_confirmed_attempt=$4
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND attempt=$4
 AND last_uncertain_attempt=$4 AND state IN ('failed','cancelled') AND message_id=0`,
			current.BotID, current.Operation, current.Effect, current.Attempt)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
		if updated.RowsAffected() != 1 {
			return botdelivery.ErrBinding
		}
		return core.DatabaseOperationError(tx.Commit(ctx))
	}
	if fallback || outcome.Kind != delivery.Succeeded || outcome.MessageID <= 0 {
		return botdelivery.ErrBinding
	}
	updated, err := tx.Exec(ctx, `UPDATE bot.delivery_intents SET message_id=$5
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND attempt=$4
 AND last_uncertain_attempt=$4 AND state IN ('failed','cancelled')
 AND last_confirmed_attempt IS DISTINCT FROM $4
 AND (message_id=0 OR message_id=$5)`,
		current.BotID, current.Operation, current.Effect, current.Attempt, outcome.MessageID)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if updated.RowsAffected() != 1 {
		return botdelivery.ErrBinding
	}
	return core.DatabaseOperationError(tx.Commit(ctx))
}

func (b *Bot) postponeBotIntent(
	ctx context.Context,
	observed botdelivery.Intent,
	terminal bool,
	failureReason ...string,
) error {
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
	if len(failureReason) != 0 {
		state, reason = delivery.Rejected, failureReason[0]
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
// ownership. Interrupted wire admissions retain uncertainty and the resend budget.
func (b *Bot) RecoverBotIntents(ctx context.Context) error {
	if err := b.Delivery.Validate(); err != nil {
		return err
	}
	for {
		tx, err := b.DB.Begin(ctx)
		if err != nil {
			return core.DatabaseOperationError(err)
		}
		var operation, effect, reason string
		err = tx.QueryRow(ctx, `SELECT operation_key,effect_key,CASE WHEN state='unknown' THEN reason ELSE 'delivery_interrupted' END FROM bot.delivery_intents
 WHERE bot_id=$1 AND (state='sending' OR (state='unknown' AND reason IN ('delivery_interrupted','telegram_outcome_unknown')))
	 ORDER BY operation_key,effect_key FOR UPDATE SKIP LOCKED LIMIT 1`, b.Delivery.BotID).
			Scan(&operation, &effect, &reason)
		if errors.Is(err, pgx.ErrNoRows) {
			_ = tx.Rollback(ctx)
			return nil
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return core.DatabaseOperationError(err)
		}
		ref := delivery.Reference{Owner: delivery.Bot, Key: operation, Effect: effect}
		current, readErr := botdelivery.Read(ctx, tx, b.Delivery.BotID, ref, true)
		if readErr != nil {
			_ = tx.Rollback(ctx)
			return readErr
		}
		err = delivery.Project(ctx, tx, b.Delivery.BotID, ref, delivery.Uncertain, time.Time{})
		if err == nil {
			outcome, deadline, retryErr := b.retryBotTransport(ctx, tx, current,
				delivery.Outcome{Kind: delivery.Uncertain, Reason: reason}, time.Time{})
			if retryErr != nil {
				_ = tx.Rollback(ctx)
				return retryErr
			}
			_, err = tx.Exec(ctx, `UPDATE bot.delivery_intents SET state=$4,reason=$5,not_before=$6
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`, b.Delivery.BotID, operation, effect, outcome.Kind, outcome.Reason, deadline)
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

// retryBotTransport changes scheduling, never the recorded factual wire outcome.
// The intent row is already locked by completion or exclusive runtime recovery.
func (b *Bot) retryBotTransport(ctx context.Context, tx pgx.Tx, current botdelivery.Intent,
	outcome delivery.Outcome, deadline time.Time,
) (delivery.Outcome, time.Time, error) {
	uncertain := outcome.Kind == delivery.Uncertain &&
		(outcome.Reason == "telegram_outcome_unknown" || outcome.Reason == "delivery_interrupted")
	if uncertain {
		_, err := tx.Exec(ctx, `UPDATE bot.delivery_intents SET last_uncertain_attempt=$4,
 last_uncertain_reason=$5,last_uncertain_recorded_at=clock_timestamp()
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
			current.BotID, current.Operation, current.Effect, current.Attempt, outcome.Reason)
		if err != nil {
			return outcome, deadline, core.DatabaseOperationError(err)
		}
	}
	var chain bool
	var resends int
	var now time.Time
	if err := tx.QueryRow(ctx, `SELECT last_uncertain_attempt IS NOT NULL,uncertain_resends,clock_timestamp()
 FROM bot.delivery_intents WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3`,
		current.BotID, current.Operation, current.Effect).Scan(&chain, &resends, &now); err != nil {
		return outcome, deadline, core.DatabaseOperationError(err)
	}
	if !chain || (!uncertain && outcome.Kind != delivery.Deferred) {
		return outcome, deadline, nil
	}
	const maxUncertainResends = 3
	if resends >= maxUncertainResends {
		outcome = delivery.Outcome{Kind: delivery.Rejected, Reason: "telegram_uncertain_retry_exhausted"}
		return outcome, deadline, delivery.Project(
			ctx,
			tx,
			current.BotID,
			current.QueueReference(),
			outcome.Kind,
			deadline,
		)
	}
	base := b.Delivery.UncertaintyRetryBaseOrDefault()
	seconds := int64(base / time.Second)
	if base%time.Second != 0 {
		seconds++
	}
	backoff, valid := delivery.Deadline(now, seconds<<resends)
	if !valid {
		return outcome, deadline, delivery.ErrQueueState
	}
	if backoff.After(deadline) {
		deadline = backoff
	}
	outcome.Kind = delivery.Deferred
	return outcome, deadline, delivery.Project(ctx, tx, current.BotID, current.QueueReference(), outcome.Kind, deadline)
}
