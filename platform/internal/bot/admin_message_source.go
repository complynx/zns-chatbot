package bot

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) registerAdminMessageSource(ctx context.Context, owner, key string, message telegram.Message) error {
	formatted, err := telegram.MessageHTML(message)
	if err != nil {
		return err
	}
	body, err := json.Marshal(
		adminmessage.Source{Actor: owner, Key: key, ChatID: message.Chat.ID, MessageID: message.ID, HTML: formatted},
	)
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/source",
		body,
		&result,
	)
}
