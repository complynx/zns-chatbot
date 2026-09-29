package bot

import (
	"context"
)

// The durable terminal plan prevents replay; the existing card hash deduplicates delivery.
func (b *Bot) deliverPassTerminalNotice(ctx context.Context, in incoming) error {
	return b.Render(ctx, in.owner, in.chat)
}
