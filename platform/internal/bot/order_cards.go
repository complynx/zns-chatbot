package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/botdelivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) deliverOrderCard(ctx context.Context, owner, key string, payload telegram.Send) error {
	return b.deliverOrderCardChecked(ctx, owner, key, payload, nil)
}

func (b *Bot) deliverOrderCardChecked(
	ctx context.Context, owner, key string, payload telegram.Send, check func() error,
) error {
	if check != nil {
		if err := check(); err != nil {
			return err
		}
	}
	ref, err := b.orderCardReference(ctx, owner, key)
	if err != nil {
		return err
	}
	if retired, ok := ctx.Value(botRetiredCardKey{}).(bool); ok {
		ref.Continuation.Retired = retired
	}
	hash, err := botCardHash(payload)
	if err != nil {
		return err
	}
	var previous string
	err = b.DB.QueryRow(ctx, "SELECT message_id,view_hash FROM bot.order_cards WHERE owner=$1 AND card_key=$2", owner, key).
		Scan(&payload.MessageID, &previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return core.DatabaseOperationError(err)
	}
	if payload.MessageID > 0 && previous == hash && ref.PaymentOpening == nil {
		return nil
	}
	return b.queueBotCard(
		ctx,
		owner,
		payload,
		ref,
		botdelivery.Continuation{Kind: "order_card", Key: key, ViewHash: hash, Retired: ref.Continuation.Retired},
	)
}
func (b *Bot) retireOrderCards(
	ctx context.Context,
	owner string,
	chat int64,
	active, available map[string]bool,
	language string,
) error {
	rows, err := b.DB.Query(ctx, `SELECT card_key FROM bot.order_cards WHERE owner=$1 AND visible`, owner)
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	for _, key := range keys {
		if strings.HasPrefix(key, mediaPrefix) || strings.HasPrefix(key, knowledgePrefix) ||
			strings.HasPrefix(key, foodPrefix) {
			continue
		}
		if active[key] {
			continue
		}
		if err = b.retireOrderCard(ctx, owner, chat, key, available[key], language); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) reconcileOrderViews(ctx context.Context) error {
	rows, err := b.DB.Query(
		ctx,
		`SELECT DISTINCT owner,chat_id FROM bot.order_cards WHERE card_key NOT IN ('language','profile') AND card_key NOT LIKE 'media:%' AND card_key NOT LIKE 'knowledge:%' AND card_key NOT LIKE 'food:%'`,
	)
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
	for _, view := range views {
		viewContext, authErr := b.API.NotificationContext(ctx, view.Owner, view.Chat)
		if authErr != nil {
			if failure := reconcileDatabaseFailure(authErr); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "order view identity pending")
			continue
		}
		if err = b.RenderOrders(viewContext, view.Owner, view.Chat); err != nil {
			if failure := reconcileDatabaseFailure(err); failure != nil {
				return failure
			}
			b.logger().WarnContext(ctx, "order view reconciliation pending", "error", err)
		}
	}
	return nil
}

func (b *Bot) retireOrderCard(
	ctx context.Context,
	owner string,
	chat int64,
	key string,
	available bool,
	language string,
) error {
	ctx = context.WithValue(ctx, botRetiredCardKey{}, true)
	text, err := i18n.Translate(language, i18n.OrderRetired, nil)
	if err != nil {
		return err
	}
	if available || strings.HasPrefix(key, orderPagerPrefix) {
		textID := i18n.OrderOffPage
		if strings.HasPrefix(key, orderPagerPrefix) {
			textID = i18n.OrderPageUnavailable
		}
		text, err = i18n.Translate(language, textID, nil)
		if err != nil {
			return err
		}
	} else if id, payment := strings.CutPrefix(key, paymentCardPrefix); payment {
		_, err = b.paymentInstructionsUnavailable(
			ctx,
			incoming{owner: owner, chat: chat},
			id,
			"payment_context_unavailable",
		)
		return err
	}
	return b.deliverOrderCard(
		ctx,
		owner,
		key,
		telegram.Send{ChatID: chat, Text: text, Markup: telegram.Markup{Rows: [][]telegram.Button{}}},
	)
}
