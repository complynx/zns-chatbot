package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) registerAdminMessageSource(ctx context.Context, owner, key string, message telegram.Message) error {
	formatted, err := telegram.MessageHTML(message)
	if err != nil {
		return err
	}
	return b.Host.RegisterAdminMessageSource(
		ctx,
		adminmessage.Source{Actor: owner, Key: key, ChatID: message.Chat.ID, MessageID: message.ID, HTML: formatted},
	)
}
