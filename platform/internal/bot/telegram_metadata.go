package bot

import (
	"context"
	"math"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) recordPrivateUpdate(ctx context.Context, in incoming, update telegram.Update) error {
	if err := b.refreshTelegramMetadata(ctx, in, update); err != nil {
		return err
	}
	return b.archiveControl(ctx, in, update)
}

// Only the accepted private sender is authoritative. Message authors on callbacks,
// forwarded identities and contacts must never rename the current user.
func (b *Bot) refreshTelegramMetadata(ctx context.Context, in incoming, update telegram.Update) error {
	if update.ID < 0 || update.ID == math.MaxInt64 {
		return nil
	}
	var sender telegram.User
	if update.Callback != nil {
		sender = update.Callback.From
	} else if update.Message != nil {
		sender = update.Message.From
	}
	if !core.ValidTelegramMetadata(sender) {
		return nil
	}
	return b.API.RefreshTelegramMetadata(
		ctx,
		in.owner,
		core.TelegramMetadataUpdate{Sender: sender, UpdateID: update.ID},
	)
}
