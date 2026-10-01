package bot

import (
	"context"
	"errors"
	"reflect"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const passSourceStale = "source_stale"

func stalePassMenuSource(err error) bool {
	if core.IsDatabaseFailure(err) {
		return false
	}
	problem, ok := errors.AsType[*core.ProblemError](err)
	return ok &&
		(problem.Code == passSourceStale || problem.Code == "pass_source_stale" || problem.Code == botdelivery.PassMenuDeniedCode || problem.Code == historyStale)
}

func stalePassCardBinding(err error) bool {
	if core.IsDatabaseFailure(err) {
		return false
	}
	problem, ok := errors.AsType[*core.ProblemError](err)
	return errors.Is(err, botdelivery.ErrStale) ||
		(ok && problem.Status == botdelivery.ErrStale.Status && problem.Code == botdelivery.ErrStale.Code)
}

// Revocation retires the original view without turning its inputs into manual data.
// A later grant restoration cannot resurrect the removed content or callbacks.
func (b *Bot) redactPassMenu(
	ctx context.Context, owner string, chat, revision int64, saved botdelivery.PassMenu,
) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tombstone := botdelivery.PassMenu{Source: saved.Source, Redacted: true}
	result, err := tx.Exec(ctx, `UPDATE bot.pass_views SET state=$4,view_hash=''
 WHERE owner=$1 AND revision=$2 AND state->'source'=$3 AND NOT COALESCE((state->>'redacted')::boolean,false)`, owner, revision, saved.Source, tombstone)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if result.RowsAffected() == 0 {
		return nil // A newer or already redacted view owns the card now.
	}
	_, err = tx.Exec(ctx, `DELETE FROM bot.pass_buttons WHERE owner=$1 AND revision=$2`, owner, revision)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	if err = tx.Commit(ctx); err != nil {
		return core.DatabaseOperationError(err)
	}
	return b.renderRedactedPassMenu(ctx, owner, chat, revision, tombstone)
}

// Only an existing card is edited. No saved body, reply or keyboard is reused,
// and a provider refusal never falls back to sending the old derived payload.
func (b *Bot) renderRedactedPassMenu(
	ctx context.Context, owner string, chat, revision int64, saved botdelivery.PassMenu,
) error {
	// Keep the existing durable reply invalidation path: removing the card must
	// also retire saved derived output so retries cannot expose it after restart.
	if _, err := b.derivedReplyVisible(ctx, owner, revision); err != nil {
		return err
	}
	live, err := b.API.NotificationContext(ctx, owner, chat)
	if err != nil {
		return err
	}
	preferences, err := b.API.Preferences(live, owner)
	if err != nil {
		return err
	}
	text, err := i18n.Translate(preferences.Language, i18n.RegistrationUnavailable, nil)
	if err != nil {
		return err
	}
	payload := telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}}
	// Recheck the exact tombstone after identity resolution, immediately before
	// the provider call. Do not hold a database transaction over the network.
	err = b.DB.QueryRow(live, `SELECT message_id FROM bot.pass_views
 WHERE owner=$1 AND chat_id=$2 AND revision=$3 AND state=$4`, owner, chat, revision, saved).
		Scan(&payload.MessageID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil || payload.MessageID == 0 {
		return core.DatabaseOperationError(err)
	}
	return b.queueBotCard(
		live,
		owner,
		payload,
		botdelivery.Reference{Family: botFamilyPassRedaction, CardKey: botFamilyPasses, Revision: revision},
		botdelivery.Continuation{Kind: botFamilyPassRedaction, Revision: revision},
	)
}

// Retire the exact source-bound menu before cancelling its stale delivery.
// Run outside card capture so the redaction queues its own edit-only effect.
func (b *Bot) retireStalePassCard(ctx context.Context, i botdelivery.Intent, cause error) error {
	result, err := b.Host.BeginBotDelivery(ctx, botdelivery.BeginRequest{Observed: i, PreparationFailure: true})
	if err != nil {
		return err
	}
	if result.Intent.State == delivery.Cancelled {
		return nil
	}
	// Generic stale bindings also include harmless target/capture changes. Only
	// retire those when the immutable original source is actually unavailable.
	_, definitive := errors.AsType[*botdelivery.PassMenuDeniedError](cause)
	if stalePassCardBinding(cause) && !definitive && !stalePassMenuSource(cause) {
		if err = b.checkPassDeliverySource(ctx, i.Owner, i.Reference.Source); err != nil {
			if !stalePassMenuSource(err) {
				return err
			}
		} else {
			return nil
		}
	}
	saved, revision, err := b.passMenuRecord(ctx, i.Owner)
	if err != nil {
		return err
	}
	if revision != i.Reference.Revision || !reflect.DeepEqual(saved.Source, i.Reference.Source) {
		return nil
	}
	if saved.Redacted {
		return b.renderRedactedPassMenu(ctx, i.Owner, i.Chat, revision, saved)
	}
	return b.redactPassMenu(ctx, i.Owner, i.Chat, revision, saved)
}
