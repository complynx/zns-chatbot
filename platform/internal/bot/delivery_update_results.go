package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"
)

// queueBotUpdateResult requires the host-bound ingress identity; callers cannot
// create an ordinary result without a stable update and private owner.
func (b *Bot) queueBotUpdateResult(
	ctx context.Context,
	chat int64,
	effect string,
	ref botdelivery.Reference,
	result botdelivery.StoredResult,
) error {
	origin, ok := ctx.Value(broadcastSourceKey{}).(broadcastSource)
	if !ok || origin.owner == "" || origin.owner != origin.in.owner || origin.in.chat != chat {
		return botdelivery.ErrBinding
	}
	return b.queueBotResult(ctx, origin.owner, chat, origin.update.ID, effect, ref, result, 0)
}
