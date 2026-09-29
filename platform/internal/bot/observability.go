package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// Observer measures an operation without receiving user identifiers or content.
type Observer interface {
	Start(context.Context, string) (context.Context, func(error))
}

func (b *Bot) Handle(ctx context.Context, update telegram.Update) error {
	if b.Observer == nil {
		return b.handle(ctx, update)
	}
	ctx, finish := b.Observer.Start(ctx, "telegram.update")
	err := b.handle(ctx, update)
	finish(err)
	if err != nil {
		b.logger().WarnContext(ctx, "telegram update failed", "error", err)
	} else {
		b.logger().DebugContext(ctx, "telegram update complete")
	}
	return err
}
