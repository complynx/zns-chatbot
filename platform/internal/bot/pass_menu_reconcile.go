package bot

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func (b *Bot) reconcilePassMenus(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.pass_views ORDER BY owner`)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	type opened struct {
		Owner string
		Chat  int64
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[opened])
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	for _, view := range views {
		viewContext, authErr := b.API.NotificationContext(ctx, view.Owner, view.Chat)
		if authErr != nil {
			if failure := reconcileDatabaseFailure(authErr); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "pass menu identity pending")
			continue
		}
		if err = b.RenderPassMenu(viewContext, view.Owner, view.Chat, ""); err != nil {
			if failure := reconcileDatabaseFailure(err); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "pass menu refresh pending")
		}
	}
	return nil
}
