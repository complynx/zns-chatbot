package bot

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/complynx/zns-chatbot/platform/internal/bot/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) foodNotificationWire(
	ctx context.Context,
	notice legacyfood.Notification,
	payload foodNotificationPayload,
) (*notificationwire.Payload, error) {
	if notice.Wire != nil {
		return notice.Wire, nil
	}
	send, err := b.foodNotificationSend(ctx, notice, payload)
	if err != nil {
		return nil, err
	}
	return notificationWireCandidate(send)
}

func (b *Bot) DeliverFoodNotifications(ctx context.Context) error {
	notices, err := b.Host.FoodNotifications(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, notice := range notices {
		err = b.deliverFoodNotification(ctx, notice)
		failures = errors.Join(failures, deliveryFailure(err))
		if core.IsDatabaseFailure(err) || ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}

func (b *Bot) deferFoodNotification(ctx context.Context, n legacyfood.Notification, err error) error {
	if core.IsDatabaseFailure(err) {
		return err
	}
	if n.FollowupPending {
		done, reason := notificationFollowupResult(err)
		return b.Host.CompleteFoodNotificationFollowup(
			ctx,
			legacyfood.NotificationFollowup{ID: n.ID, Attempt: n.DeliveryAttempt, Done: done, Failure: reason},
		)
	}
	return b.Host.CompleteFoodNotification(
		ctx,
		legacyfood.NotificationCompletion{
			ID:      n.ID,
			Attempt: n.DeliveryAttempt,
			Outcome: notificationPreflightOutcome(err),
		},
	)
}

type foodNotificationPayload struct {
	OrderID    string `json:"order_id"`
	Kind       string `json:"kind"`
	Generation int64  `json:"generation"`
}

func (b *Bot) deliverFoodNotification(ctx context.Context, notice legacyfood.Notification) error {
	if !notice.Current {
		if notice.FollowupPending {
			return b.Host.CompleteFoodNotificationFollowup(
				ctx,
				legacyfood.NotificationFollowup{
					ID:      notice.ID,
					Attempt: notice.DeliveryAttempt,
					Done:    true,
					Failure: notificationNoLongerCurrent,
				},
			)
		}
		return b.Host.CompleteFoodNotification(
			ctx,
			legacyfood.NotificationCompletion{
				ID:      notice.ID,
				Attempt: notice.DeliveryAttempt,
				Outcome: delivery.Outcome{Kind: delivery.Cancelled, Reason: notificationNoLongerCurrent},
			},
		)
	}
	authenticated, err := b.API.NotificationContext(ctx, notice.Owner, notice.TelegramID)
	if err != nil {
		return b.deferFoodNotification(ctx, notice, err)
	}
	ctx = authenticated
	var payload foodNotificationPayload
	if err = json.Unmarshal(notice.Payload, &payload); err != nil {
		return b.deferFoodNotification(ctx, notice, err)
	}
	var known bool
	notice, known, err = b.sendPreparedFoodNotice(ctx, notice, payload)
	if err != nil || !known {
		return err
	}
	ctx = withBotDeliveryOrigin(
		ctx,
		delivery.Reference{Owner: delivery.Food, Key: strconv.FormatInt(notice.ID, 10), Effect: botRefreshEffect},
	)
	err = dbgen.New(b.DB).
		StoreFoodNotificationReceipt(ctx, dbgen.StoreFoodNotificationReceiptParams{Owner: notice.Owner, Key: "food:notification:" + strconv.FormatInt(notice.ID, 10), Chat: notice.TelegramID, MessageID: notice.MessageID})
	err = core.DatabaseOperationError(err)
	if err == nil && payload.OrderID != "" {
		err = b.renderFood(
			ctx,
			incoming{owner: notice.Owner, chat: notice.TelegramID},
			notice.EventID,
			payload.OrderID,
			notice.Kind == legacyfood.Submitted,
		)
	}
	if core.IsDatabaseFailure(err) {
		return err
	}
	done, reason := notificationFollowupResult(err)
	completeErr := b.Host.CompleteFoodNotificationFollowup(
		ctx,
		legacyfood.NotificationFollowup{ID: notice.ID, Attempt: notice.DeliveryAttempt, Done: done, Failure: reason},
	)
	return errors.Join(err, completeErr)
}

func (b *Bot) foodNotificationSend(
	ctx context.Context,
	notice legacyfood.Notification,
	payload foodNotificationPayload,
) (telegram.Send, error) {
	pref, err := b.API.Preferences(ctx, notice.Owner)
	if err != nil {
		return telegram.Send{}, err
	}
	status, err := i18n.Translate(pref.Language, i18n.ID("food.status."+notice.Kind), nil)
	if err != nil {
		return telegram.Send{}, err
	}
	text, err := i18n.Translate(pref.Language, i18n.FoodNotice, map[string]string{"notice": status})
	if err != nil {
		return telegram.Send{}, err
	}
	markup := telegram.Markup{Rows: [][]telegram.Button{}}
	if notice.Kind == legacyfood.Submitted {
		view, viewErr := b.API.FoodViewForReview(ctx, notice.Owner, notice.EventID, payload.OrderID, true)
		if viewErr != nil {
			return telegram.Send{}, viewErr
		}
		markup, err = b.foodMarkup(ctx, notice.Owner, pref.Language, view.Order, true, true)
		if err != nil {
			return telegram.Send{}, err
		}
	}
	return telegram.Send{ChatID: notice.TelegramID, Text: text, Markup: markup}, nil
}

// A known canonical outcome permits follow-up; deferred or rejected work does not.
func (b *Bot) sendPreparedFoodNotice(
	ctx context.Context,
	notice legacyfood.Notification,
	payload foodNotificationPayload,
) (legacyfood.Notification, bool, error) {
	if notice.FollowupPending {
		return notice, true, nil
	}
	wire, err := b.foodNotificationWire(ctx, notice, payload)
	if err != nil {
		return notice, false, b.deferFoodNotification(ctx, notice, err)
	}
	gate, err := b.Host.BeginFoodNotification(
		ctx,
		legacyfood.NotificationAttempt{
			Attempt: delivery.Attempt{ID: notice.ID, Generation: notice.DeliveryAttempt},
			Wire:    wire,
		},
	)
	if err != nil || !gate.Ready {
		return notice, false, err
	}
	send, err := notificationWireSend(notice.TelegramID, gate.Wire)
	if err != nil {
		return notice, false, err
	}
	message, sendErr := b.TG.Send(ctx, send)
	outcome := telegram.DeliveryOutcome(message.ID, sendErr)
	result := legacyfood.NotificationCompletion{ID: notice.ID, Attempt: notice.DeliveryAttempt, Outcome: outcome}
	if outcome.Kind == delivery.Succeeded {
		result.Text = send.Text
	}
	completionCtx, cancelCompletion := deliveryCompletionContext(ctx)
	defer cancelCompletion()
	if err = b.Host.CompleteFoodNotification(completionCtx, result); err != nil {
		return notice, false, err
	}
	if outcome.Kind != delivery.Succeeded {
		return notice, false, nil
	}
	notice.MessageID = message.ID
	return notice, true, nil
}

// DeliverFoodNotification handles one advisory shared-queue candidate.
func (b *Bot) DeliverFoodNotification(ctx context.Context, id int64) error {
	notice, found, err := b.Host.PrepareFoodNotification(ctx, id)
	if err != nil || !found {
		return err
	}
	return b.deliverFoodNotification(ctx, notice)
}

// RecoverFoodNotifications never starts a fresh primary send.
func (b *Bot) RecoverFoodNotifications(ctx context.Context) error {
	notices, err := b.Host.RecoverFoodNotifications(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, notice := range notices {
		err = b.deliverFoodNotification(ctx, notice)
		failures = errors.Join(failures, deliveryFailure(err))
		if core.IsDatabaseFailure(err) || ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}
