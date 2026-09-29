package bot

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) deliverOrderCard(ctx context.Context, owner, key string, payload telegram.Send) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	var previous string
	err = b.DB.QueryRow(ctx, `SELECT message_id,view_hash FROM bot.order_cards WHERE owner=$1 AND card_key=$2`, owner, key).
		Scan(&payload.MessageID, &previous)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if previous == hash {
		return nil
	}
	id, err := b.editOrSend(ctx, payload)
	if err != nil {
		return err
	}
	_, err = b.DB.Exec(
		ctx,
		`INSERT INTO bot.order_cards(owner,card_key,chat_id,message_id,view_hash) VALUES($1,$2,$3,$4,$5)
		ON CONFLICT(owner,card_key) DO UPDATE SET chat_id=$3,message_id=$4,view_hash=$5,visible=true`,
		owner,
		key,
		payload.ChatID,
		id,
		hash,
	)
	return err
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
		return err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
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
		if _, err = b.DB.Exec(
			ctx,
			`UPDATE bot.order_cards SET visible=false WHERE owner=$1 AND card_key=$2`,
			owner,
			key,
		); err != nil {
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
	for _, view := range views {
		viewContext, authErr := b.API.notificationContext(ctx, view.Owner, view.Chat)
		if authErr != nil {
			b.logger().WarnContext(ctx, "order view identity pending")
			continue
		}
		if err = b.RenderOrders(viewContext, view.Owner, view.Chat); err != nil {
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
