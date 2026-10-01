package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/core"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// DeliverBotIntent handles one exact shared-queue reference. The runtime owns
// selection, cancellation and joining; this method never creates a worker.
func (b *Bot) DeliverBotIntent(ctx context.Context, ref delivery.Reference) error {
	if ref.Owner != delivery.Bot {
		return botdelivery.ErrBinding
	}
	if err := b.Delivery.Validate(); err != nil {
		return err
	}
	i, err := botdelivery.Read(ctx, b.DB, b.Delivery.BotID, ref, false)
	if err != nil {
		return err
	}
	if i.State == delivery.Succeeded {
		return b.continueBotIntent(ctx, i)
	}
	if i.State != delivery.Deferred {
		return nil
	}
	live := ctx
	if i.Reference.Kind != botdelivery.IdentityIntent {
		live, err = b.API.NotificationContext(ctx, i.Owner, i.Chat)
		if err != nil {
			return b.botPreparationFailure(ctx, i, err)
		}
	}
	rendered, err := b.renderBotIntent(live, i)
	if err != nil {
		return b.botPreparationFailure(ctx, i, err)
	}
	if i.Phase != botDocumentKind {
		rendered.Payload, err = telegram.PrepareSend(rendered.Payload)
		if err != nil {
			return b.botPreparationFailure(ctx, i, botdelivery.ErrStale)
		}
	}
	attempt, ready, err := b.beginBotIntent(live, i, rendered)
	if err != nil {
		return b.botPreparationFailure(ctx, i, err)
	}
	if !ready {
		return nil
	}
	outcome, fallback := b.sendBotIntent(live, attempt, rendered)
	// A known provider response must be persisted even when shutdown cancelled
	// the caller while the response was being read.
	cleanup, cancel := deliveryCompletionContext(live)
	defer cancel()
	if err = b.finishBotIntent(cleanup, attempt, outcome, rendered.Receipt, fallback); err != nil {
		return err
	}
	if outcome.Kind != delivery.Succeeded {
		return nil
	}
	completed, err := botdelivery.Read(cleanup, b.DB, attempt.BotID, ref, false)
	if err != nil {
		return err
	}
	if completed.State != delivery.Succeeded {
		return nil
	}
	return b.continueBotIntent(cleanup, completed)
}

func (b *Bot) sendBotIntent(ctx context.Context, i botdelivery.Intent, r botRenderedDelivery) (delivery.Outcome, bool) {
	switch i.Phase {
	case botDocumentKind:
		message, err := b.TG.SendDocument(ctx, i.Chat, r.Filename, r.Body)
		return telegram.DeliveryOutcome(message.ID, err), false
	case botPhaseEdit:
		r.Payload.ChatID, r.Payload.MessageID = i.Chat, i.Target
		err := b.TG.Edit(ctx, r.Payload)
		if err == nil {
			return telegram.DeliveryOutcome(i.Target, nil), false
		}
		fallback, editErr := passMenuEditFallback(err)
		if editErr == nil && !fallback {
			return telegram.DeliveryOutcome(i.Target, nil), false
		}
		if fallback &&
			(i.Reference.Family == botFamilyPassRedaction || i.Reference.Family == botdelivery.PassReceiptRedactionFamily ||
				(i.Reference.Family == registrationPayment && i.Reference.Notice == i18n.PaymentUnavailable)) {
			return delivery.Outcome{Kind: delivery.Rejected, Reason: "edit_target_missing"}, false
		}
		if fallback {
			return delivery.Outcome{Kind: delivery.Deferred, Reason: "edit_target_missing"}, true
		}
		return telegram.DeliveryOutcome(0, err), false
	default:
		r.Payload.ChatID, r.Payload.MessageID = i.Chat, 0
		message, err := b.TG.Send(ctx, r.Payload)
		return telegram.DeliveryOutcome(message.ID, err), false
	}
}

func (b *Bot) botPreparationFailure(ctx context.Context, i botdelivery.Intent, cause error) error {
	if core.IsDatabaseFailure(cause) {
		return core.ErrDatabase
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	// Retire at the shared failure boundary, after reconstruction or admission.
	// The immutable reference can only retire its exact saved source and revision.
	retirePass := passCardRetirementRequired(i, cause)
	if retirePass {
		if err := b.retireStalePassCard(ctx, i, cause); err != nil {
			return err
		}
	}
	terminal := retirePass || errors.Is(cause, botdelivery.ErrBinding) ||
		errors.Is(cause, botdelivery.ErrStale) ||
		errors.Is(cause, pgx.ErrNoRows) ||
		errors.Is(cause, identity.ErrZitadelUserInactive) ||
		errors.Is(cause, identity.ErrZitadelIdentity) ||
		errors.Is(cause, errHistoryPlanTerminal) ||
		errors.Is(cause, errPassPlanTerminal) ||
		// A corrupted current-format plan is permanent; unsupported formats and
		// database or decode failures remain retryable and visible.
		errors.Is(cause, interaction.ErrInvalidSavedTurn) ||
		orderDeliveryDenied(cause)
	if err := b.postponeBotIntent(ctx, i, terminal); err != nil {
		return err
	}
	if terminal {
		return nil
	}
	return cause
}

// ContinueBotIntentReceipts retries only post-receipt effects. Sent transport
// slots are already terminal, so a refresh child cannot deadlock behind its parent.
func (b *Bot) ContinueBotIntentReceipts(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT operation_key,effect_key FROM bot.delivery_intents
 WHERE bot_id=$1 AND state='sent' AND NOT continuation_done AND not_before<=clock_timestamp() ORDER BY not_before,created_at,operation_key,effect_key LIMIT 32`, b.Delivery.BotID)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	type key struct{ operation, effect string }
	var keys []key
	for rows.Next() {
		var k key
		if err = rows.Scan(&k.operation, &k.effect); err != nil {
			rows.Close()
			return core.DatabaseOperationError(err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return core.DatabaseOperationError(err)
	}
	var failures []error
	for _, k := range keys {
		i, readErr := botdelivery.Read(
			ctx,
			b.DB,
			b.Delivery.BotID,
			delivery.Reference{Owner: delivery.Bot, Key: k.operation, Effect: k.effect},
			false,
		)
		if readErr != nil {
			if core.IsDatabaseFailure(readErr) {
				return errors.Join(append(failures, core.ErrDatabase)...)
			}
			failures = append(failures, readErr)
			continue
		}
		if err = b.continueBotIntent(ctx, i); err != nil {
			if core.IsDatabaseFailure(err) {
				return errors.Join(append(failures, core.ErrDatabase)...)
			}
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}
func (b *Bot) continueBotIntent(ctx context.Context, observed botdelivery.Intent) error {
	if err := b.Delivery.Validate(); err != nil {
		return err
	}
	err := b.applyBotIntentReceipt(ctx, observed)
	if err == nil {
		return nil
	}
	if core.IsDatabaseFailure(err) {
		return core.ErrDatabase
	}
	// Shutdown or a caller deadline is not an unavailable boundary: the next
	// runtime continues the receipt without a postponement or extra SQL.
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	// Retain the sent receipt for recovery, but let older ready receipts advance
	// before retrying an unavailable identity or application boundary.
	cleanup, cancel := deliveryCompletionContext(ctx)
	defer cancel()
	_, retryErr := b.DB.Exec(cleanup, `UPDATE bot.delivery_intents
 SET not_before=GREATEST(not_before,clock_timestamp()+$6::bigint*interval '1 microsecond')
 WHERE bot_id=$1 AND operation_key=$2 AND effect_key=$3 AND state='sent'
 AND NOT continuation_done AND attempt=$4 AND message_id=$5`,
		observed.BotID, observed.Operation, observed.Effect, observed.Attempt,
		observed.MessageID, b.Delivery.Fallback.Microseconds())
	return errors.Join(err, core.DatabaseOperationError(retryErr))
}

func (b *Bot) applyBotIntentReceipt(ctx context.Context, observed botdelivery.Intent) error {
	if observed.Reference.Kind != botdelivery.IdentityIntent {
		live, err := b.API.NotificationContext(ctx, observed.Owner, observed.Chat)
		if err != nil {
			return err
		}
		ctx = live
	}
	return b.Host.ApplyBotDeliveryReceipt(ctx, botdelivery.ReceiptRequest{Observed: observed})
}

func passCardRetirementRequired(i botdelivery.Intent, cause error) bool {
	if i.Reference.Family != botFamilyPasses || i.Reference.Source == nil ||
		core.IsDatabaseFailure(cause) {
		return false
	}
	if _, ok := errors.AsType[*botdelivery.PassMenuDeniedError](cause); ok {
		return true
	}
	if stalePassCardBinding(cause) || stalePassMenuSource(cause) {
		return true
	}
	problem, ok := errors.AsType[*core.ProblemError](cause)
	return ok && problem.Status == 403
}
