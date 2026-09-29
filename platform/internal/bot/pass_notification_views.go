package bot

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (b *Bot) refreshPassNotificationViews(ctx context.Context, notice passbooking.Notification) error {
	var err error
	var opened bool
	if err = b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.pass_views WHERE owner=$1)`, notice.Recipient).
		Scan(&opened); err != nil {
		return err
	}
	if opened {
		if err = b.RenderPassMenu(ctx, notice.Recipient, notice.TelegramID, ""); err != nil {
			return err
		}
	}
	if notice.Kind == "passport_required" {
		if err = b.RenderProfile(ctx, notice.Recipient, notice.TelegramID); err != nil {
			return err
		}
	}
	return nil
}
