package bot

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// reconcileDatabaseFailure stops reconciliation only for a positively classified
// SQL failure. Identity, provider and domain failures remain per-view warnings,
// while a positive SQL failure survives concurrent shutdown.
func reconcileDatabaseFailure(err error) error {
	if !core.IsDatabaseFailure(err) {
		return nil
	}
	return core.ErrDatabase
}

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
		return core.DatabaseOperationError(err)
	}
	if opened {
		return b.RenderProfile(ctx, owner, chat)
	}
	return nil
}

func (b *Bot) reconcileProfileViews(ctx context.Context) error {
	return b.reconcileNamedCard(ctx, "profile", b.RenderProfile)
}

func (b *Bot) reconcileNamedCard(
	ctx context.Context, key string, render func(context.Context, string, int64) error,
) error {
	rows, err := b.DB.Query(ctx, `SELECT owner,chat_id FROM bot.order_cards WHERE card_key=$1`, key)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	type view struct {
		Owner string
		Chat  int64
	}
	views, err := pgx.CollectRows(rows, pgx.RowToStructByPos[view])
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	for _, item := range views {
		viewContext, authErr := b.API.NotificationContext(ctx, item.Owner, item.Chat)
		if authErr != nil {
			if failure := reconcileDatabaseFailure(authErr); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "card view identity pending", "card", key)
			continue
		}
		if err = render(viewContext, item.Owner, item.Chat); err != nil {
			if failure := reconcileDatabaseFailure(err); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "card view reconciliation pending", "card", key)
		}
	}
	return nil
}
