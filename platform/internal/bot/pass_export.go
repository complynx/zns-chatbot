package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) exportPasses(ctx context.Context, in incoming, update int64) (i18n.ID, error) {
	return b.exportPassesWithSource(ctx, in, update, nil)
}

func (b *Bot) exportPassesWithSource(
	ctx context.Context,
	in incoming,
	update int64,
	source *readsource.Derivation,
) (i18n.ID, error) {
	if err := b.checkPassDeliverySource(ctx, in.owner, source); err != nil {
		return "", err
	}
	observed, err := b.queueBotDocument(
		ctx,
		in.owner,
		in.chat,
		botdelivery.Reference{
			Family:       botFamilyPassExport,
			Update:       update,
			Source:       source,
			Notice:       i18n.RegistrationExported,
			Continuation: botdelivery.Continuation{Kind: botDocumentKind, Key: botFamilyPassExport},
		},
	)
	if err != nil {
		return "", err
	}
	if documentDelivered(observed) {
		return i18n.RegistrationExported, nil
	}
	return "", nil
}

func (b *Bot) handlePassExport(ctx context.Context, in incoming, update telegram.Update) error {
	notice, err := b.exportPasses(ctx, in, update.ID)
	if err != nil {
		return err
	}
	state, _, err := b.passMenuState(ctx, in.owner)
	if err != nil {
		return err
	}
	state.Notice = notice
	if err = b.storePassMenu(ctx, in.owner, in.chat, update.ID, state); err != nil {
		return err
	}
	return b.RenderPassMenu(ctx, in.owner, in.chat, notice)
}
