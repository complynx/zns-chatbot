package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

type TelegramOnboarding func(context.Context, telegram.User) error

func (b *Bot) denyOnboarding(ctx context.Context, in incoming, update telegram.Update) error {
	if update.Callback != nil {
		b.acknowledge(ctx, update.Callback.ID)
	}
	ref := botdelivery.Reference{
		Kind:     botdelivery.IdentityIntent,
		Update:   update.ID,
		Notice:   i18n.IdentityUnavailable,
		Language: in.language,
	}
	_, err := b.enqueueBotIntent(
		ctx,
		"",
		in.chat,
		"identity:"+strconv.FormatInt(update.ID, 10),
		"unavailable",
		ref,
		botPhaseSend,
	)
	return err
}
func (b *Bot) onboardIncoming(ctx context.Context, update telegram.Update) error {
	if update.Callback != nil {
		return b.Onboarding(ctx, update.Callback.From)
	}
	if update.Message != nil {
		return b.Onboarding(ctx, update.Message.From)
	}
	return identity.ErrZitadelIdentity
}
