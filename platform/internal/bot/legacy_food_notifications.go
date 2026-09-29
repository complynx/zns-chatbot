package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) DeliverFoodNotifications(ctx context.Context) error {
	var notices []legacyfood.Notification
	if err := b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodGet,
		"/internal/food/notifications",
		nil,
		&notices,
	); err != nil {
		return err
	}
	for _, notice := range notices {
		if err := b.deliverFoodNotification(ctx, notice); err != nil {
			return err
		}
	}
	return nil
}

func (b *Bot) deliverFoodNotification(ctx context.Context, notice legacyfood.Notification) error {
	ctx, err := b.API.notificationContext(ctx, notice.Owner, notice.TelegramID)
	if err != nil {
		return err
	}
	var payload struct {
		OrderID    string `json:"order_id"`
		Kind       string `json:"kind"`
		Generation int64  `json:"generation"`
	}
	if err = json.Unmarshal(notice.Payload, &payload); err != nil {
		return err
	}
	pref, err := b.API.Preferences(ctx, notice.Owner)
	if err != nil {
		return err
	}
	status, err := i18n.Translate(pref.Language, i18n.ID("food.status."+notice.Kind), nil)
	if err != nil {
		return err
	}
	text, err := i18n.Translate(pref.Language, i18n.FoodNotice, map[string]string{"notice": status})
	if err != nil {
		return err
	}
	markup := telegram.Markup{Rows: [][]telegram.Button{}}
	if notice.Kind == legacyfood.Submitted {
		view, viewErr := b.API.foodView(ctx, notice.Owner, notice.EventID, payload.OrderID, true)
		if viewErr != nil {
			return viewErr
		}
		markup, err = b.foodMarkup(ctx, notice.Owner, pref.Language, view.Order, true, true)
		if err != nil {
			return err
		}
	}
	key := "food:notification:" + strconv.FormatInt(notice.ID, 10)
	if err = b.deliverOrderCard(
		ctx,
		notice.Owner,
		key,
		telegram.Send{ChatID: notice.TelegramID, Text: text, Markup: markup},
	); err != nil {
		return err
	}
	if payload.OrderID != "" {
		if err = b.renderFood(
			ctx,
			incoming{owner: notice.Owner, chat: notice.TelegramID},
			notice.EventID,
			payload.OrderID,
			notice.Kind == legacyfood.Submitted,
		); err != nil {
			return err
		}
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/food/notifications/"+strconv.FormatInt(notice.ID, 10)+"/complete",
		nil,
		&result,
	)
}
