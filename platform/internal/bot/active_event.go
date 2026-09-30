package bot

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) currentOrderEvent() string {
	if b.OrderEventID != "" {
		return b.OrderEventID
	}
	return "sandbox-festival"
}

// OrderEventForOrder resolves the immutable event through Core's owner authorization.
func (b *Bot) OrderEventForOrder(ctx context.Context, owner, id string) (string, error) {
	order, err := b.API.OrderByID(ctx, owner, id)
	return order.EventID, err
}

// Each delivery keeps its first event across retries. A copy avoids changing the
// process default while callbacks render and execute their originating event.
func (b *Bot) forOrderUpdate(ctx context.Context, in incoming, update telegram.Update) (*Bot, error) {
	event := b.currentOrderEvent()
	if update.Callback != nil {
		var err error
		event, err = b.orderCallbackEvent(ctx, in, update.ID, event)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if err := b.record(ctx, in.owner, update.ID, "order_event", event); err != nil {
		return nil, err
	}
	if err := b.DB.QueryRow(ctx, `SELECT content #>> '{}' FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='order_event'`, in.owner, update.ID).
		Scan(&event); err != nil {
		return nil, core.DatabaseOperationContextError(ctx, err)
	}
	scoped := *b
	scoped.OrderEventID = event
	return &scoped, nil
}

func (b *Bot) orderCallbackEvent(ctx context.Context, in incoming, update int64, event string) (string, error) {
	if strings.HasPrefix(in.text, legacyOrderPrefix) {
		receipt, err := b.bindLegacyOrder(ctx, in, update)
		if err == nil && receipt.Code == "" {
			event = receipt.Binding.EventID
		}
		return event, err
	}
	// Absence keeps the caller's default event; any other lookup failure is SQL-origin.
	var err error
	if token, ok := strings.CutPrefix(in.text, orderPagePrefix); ok {
		err = b.DB.QueryRow(ctx, `SELECT event_id FROM bot.order_page_buttons WHERE owner=$1 AND token=$2`, in.owner, token).
			Scan(&event)
	} else if orderToken, orderOK := strings.CutPrefix(in.text, orderCallbackPrefix); orderOK {
		err = b.DB.QueryRow(ctx, `SELECT command->>'event_id' FROM bot.order_buttons WHERE owner=$1 AND token=$2`, in.owner, orderToken).
			Scan(&event)
	}
	return event, core.DatabaseOperationContextError(ctx, err)
}
