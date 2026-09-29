package bot

import (
	"context"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func (b *Bot) deliverAdminInputExpiry(ctx context.Context) error {
	expiry, found, err := b.Host.ClaimAdminInputExpiry(ctx)
	if err != nil || !found {
		return err
	}
	ref := botdelivery.Reference{
		Family:       botFamilyAdminExpiry,
		Version:      expiry.ID,
		Continuation: botdelivery.Continuation{Kind: botFamilyAdminExpiry, ID: expiry.ID},
	}
	return b.queueBotResult(
		ctx,
		expiry.Actor,
		expiry.ChatID,
		0,
		"admin_expiry:"+strconv.FormatInt(expiry.ID, 10),
		ref,
		botdelivery.StoredResult{
			Notice: i18n.AdminBroadcastExpired,
			Values: map[string]string{"id": strconv.FormatInt(expiry.ID, 10)},
		}, 0,
	)
}
