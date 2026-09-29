package bot

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const passSourceStale = "source_stale"

func stalePassMenuSource(err error) bool {
	problem, ok := errors.AsType[*core.ProblemError](err)
	return ok && (problem.Code == passSourceStale || problem.Code == historyStale)
}

// Revocation retires the original view without turning its inputs into manual data.
// A later grant restoration cannot resurrect the removed content or callbacks.
func (b *Bot) redactPassMenu(
	ctx context.Context, owner string, chat, revision int64, saved botdelivery.PassMenu,
) error {
	tx, err := b.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tombstone := botdelivery.PassMenu{Source: saved.Source, Redacted: true}
	result, err := tx.Exec(ctx, `UPDATE bot.pass_views SET state=$4,view_hash=''
 WHERE owner=$1 AND revision=$2 AND state->'source'=$3 AND NOT COALESCE((state->>'redacted')::boolean,false)`, owner, revision, saved.Source, tombstone)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return nil // A newer or already redacted view owns the card now.
	}
	_, err = tx.Exec(ctx, `DELETE FROM bot.pass_buttons WHERE owner=$1 AND revision=$2`, owner, revision)
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
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
		return err
	}
	return b.queueBotCard(
		live,
		owner,
		payload,
		botdelivery.Reference{Family: botFamilyPassRedaction, CardKey: botFamilyPasses, Revision: revision},
		botdelivery.Continuation{Kind: botFamilyPassRedaction, Revision: revision},
	)
}
