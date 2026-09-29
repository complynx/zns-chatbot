package bot

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (b *Bot) reconcilePassMenus(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.pass_views ORDER BY owner`)
	if err != nil {
		return err
	}
	type opened struct {
		Owner string
		Chat  int64
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[opened])
	if err != nil {
		return err
	}
	for _, view := range views {
		viewContext, authErr := b.API.NotificationContext(ctx, view.Owner, view.Chat)
		if authErr != nil {
			b.logger().WarnContext(ctx, "pass menu identity pending")
			continue
		}
		if err = b.RenderPassMenu(viewContext, view.Owner, view.Chat, ""); err != nil {
			b.logger().WarnContext(ctx, "pass menu refresh pending")
		}
	}
	return nil
}
