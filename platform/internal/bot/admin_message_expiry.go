package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) deliverAdminInputExpiry(ctx context.Context) error {
	var claim struct {
		Expiry adminmessage.InputExpiry `json:"expiry"`
		Found  bool                     `json:"found"`
	}
	if err := b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/input-expiry/claim",
		nil,
		&claim,
	); err != nil {
		return err
	}
	if !claim.Found {
		return nil
	}
	messages := &orderMessages{language: claim.Expiry.Language}
	text := messages.text(i18n.AdminBroadcastExpired, map[string]string{"id": strconv.FormatInt(claim.Expiry.ID, 10)})
	if messages.err != nil {
		return messages.err
	}
	if _, err := b.TG.Send(ctx, telegram.Send{ChatID: claim.Expiry.ChatID, Text: text}); err != nil {
		return err
	}
	body, err := json.Marshal(map[string]int64{"id": claim.Expiry.ID, "attempt": claim.Expiry.Attempt})
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
		"/internal/admin-messages/input-expiry/complete",
		body,
		&result,
	)
}
