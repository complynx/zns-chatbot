package bot

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func (b *Bot) reconcileAllViews(ctx context.Context) error {
	if err := b.reconcilePassMenus(ctx); err != nil {
		return err
	}
	if err := b.reconcileViews(ctx); err != nil {
		return err
	}
	if err := b.reconcileOrderViews(ctx); err != nil {
		return err
	}
	if err := b.reconcileProfileViews(ctx); err != nil {
		return err
	}
	if err := b.reconcileMediaViews(ctx); err != nil {
		return err
	}
	return b.reconcileKnowledgeViews(ctx)
}

func (b *Bot) refreshOpenProfile(ctx context.Context, owner string, chat int64) error {
	var opened bool
	if err := b.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM bot.order_cards WHERE owner=$1 AND card_key='profile')`, owner).
		Scan(&opened); err != nil {
		return err
	}
	if opened {
		return b.RenderProfile(ctx, owner, chat)
	}
	return nil
}

func (b *Bot) reconcileProfileViews(ctx context.Context) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.order_cards WHERE card_key='profile'`)
	if err != nil {
		return err
	}
	type view struct {
		Owner string
		Chat  int64
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[view])
	if err != nil {
		return err
	}
	for _, item := range views {
		viewContext, authErr := b.API.notificationContext(ctx, item.Owner, item.Chat)
		if authErr != nil {
			b.logger().WarnContext(ctx, "profile view identity pending")
			continue
		}
		if err = b.RenderProfile(viewContext, item.Owner, item.Chat); err != nil {
			b.logger().WarnContext(ctx, "profile view reconciliation pending")
		}
	}
	return nil
}
