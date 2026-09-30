package bot

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// botdelivery.PassMenu keeps causal metadata outside the model-visible menu contract.

func (b *Bot) passMenuRecord(ctx context.Context, owner string) (botdelivery.PassMenu, int64, error) {
	var value botdelivery.PassMenu
	value.View = passMenuEvents
	var revision int64
	var raw []byte
	err := b.DB.QueryRow(ctx, `SELECT state,revision FROM bot.pass_views WHERE owner=$1`, owner).Scan(&raw, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, revision, nil
	}
	if err != nil {
		return value, revision, core.DatabaseOperationError(err)
	}
	value = botdelivery.PassMenu{}
	err = json.Unmarshal(raw, &value)
	return value, revision, err
}

func (b *Bot) checkPassDeliverySource(ctx context.Context, owner string, source *readsource.Derivation) error {
	if source == nil {
		return nil
	}
	return b.Host.CheckBotDeliverySource(ctx, botdelivery.SourceRequest{Owner: owner, Source: source})
}

func (b *Bot) storePassMenuWithSource(
	ctx context.Context,
	owner string,
	chat, revision int64,
	state interaction.RegistrationMenu,
	source *readsource.Derivation,
) error {
	return b.Host.StoreBotPassMenu(
		ctx,
		botdelivery.PassMenuRequest{Owner: owner, Chat: chat, Revision: revision, State: state, Source: source},
	)
}

// Check after body construction and before each provider attempt. No database
// transaction remains open while fresh identity or Telegram is contacted.
func (b *Bot) checkPassMenuDelivery(
	ctx context.Context,
	owner string,
	chat int64,
	state interaction.RegistrationMenu,
	source *readsource.Derivation,
) error {
	live, err := b.API.NotificationContext(ctx, owner, chat)
	if err != nil {
		return err
	}
	preferences, err := b.API.Preferences(live, owner)
	if err != nil {
		return err
	}
	renderer := passMenuRenderer{bot: b, owner: owner, language: preferences.Language, state: state}
	if err = renderer.build(live); err != nil {
		return err
	}
	return b.checkPassDeliverySource(live, owner, source)
}
