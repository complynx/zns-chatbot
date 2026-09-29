package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/bot/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const reminderKind = "reminder"
const notificationNoLongerCurrent = "notification_no_longer_current"

// DeliverNotifications records each outcome before unrelated recipients continue.
func (b *Bot) DeliverNotifications(ctx context.Context) error {
	notices, err := b.Host.PendingNotifications(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, notice := range notices {
		failures = errors.Join(failures, b.deliverNotification(ctx, notice))
		if ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}

func notificationPreflightOutcome(err error) delivery.Outcome {
	if p, ok := errors.AsType[*core.ProblemError](
		err,
	); ok &&
		(p.Status == http.StatusForbidden || p.Status == http.StatusNotFound || p.Status == http.StatusGone) {
		return delivery.Outcome{Kind: delivery.Cancelled, Reason: "notification_recipient_unavailable"}
	}
	return delivery.Outcome{Kind: delivery.Deferred, Reason: "notification_preflight_unavailable", Missing: true}
}

func notificationFollowupResult(err error) (bool, string) {
	if err == nil {
		return true, ""
	}
	if notificationPreflightOutcome(err).Kind == delivery.Cancelled {
		return true, "notification_recipient_unavailable"
	}
	return false, "notification_followup_unavailable"
}

func (b *Bot) deferOrderNotification(ctx context.Context, n orders.Notification, err error) error {
	if n.FollowupPending {
		done, reason := notificationFollowupResult(err)
		return b.Host.CompleteNotificationFollowup(
			ctx,
			orders.NotificationFollowup{ID: n.ID, Attempt: n.DeliveryAttempt, Done: done, Failure: reason},
		)
	}
	return b.Host.CompleteNotification(
		ctx,
		orders.NotificationCompletion{ID: n.ID, Attempt: n.DeliveryAttempt, Outcome: notificationPreflightOutcome(err)},
	)
}

func (b *Bot) deliverNotification(ctx context.Context, notice orders.Notification) error {
	if !notice.Current {
		if notice.FollowupPending {
			return b.Host.CompleteNotificationFollowup(
				ctx,
				orders.NotificationFollowup{
					ID:      notice.ID,
					Attempt: notice.DeliveryAttempt,
					Done:    true,
					Failure: notificationNoLongerCurrent,
				},
			)
		}
		return b.Host.CompleteNotification(
			ctx,
			orders.NotificationCompletion{
				ID:      notice.ID,
				Attempt: notice.DeliveryAttempt,
				Outcome: delivery.Outcome{Kind: delivery.Cancelled, Reason: notificationNoLongerCurrent},
			},
		)
	}
	authenticated, err := b.API.NotificationContext(ctx, notice.Recipient, notice.TelegramID)
	if err != nil {
		return b.deferOrderNotification(ctx, notice, err)
	}
	ctx = authenticated
	var known bool
	notice, known, err = b.sendPreparedOrderNotice(ctx, notice)
	if err != nil || !known {
		return err
	}
	ctx = withBotDeliveryOrigin(
		ctx,
		delivery.Reference{Owner: delivery.Orders, Key: strconv.FormatInt(notice.ID, 10), Effect: botRefreshEffect},
	)
	err = dbgen.New(b.DB).
		StoreOrderNotificationReceipt(ctx, dbgen.StoreOrderNotificationReceiptParams{ID: notice.ID, MessageID: notice.MessageID})
	if err == nil {
		err = b.record(
			ctx,
			notice.Recipient,
			-notice.ID,
			"order_notification",
			map[string]string{originField: "system", textField: notice.DeliveryText},
		)
	}
	if err == nil {
		err = b.RenderOrders(ctx, notice.Recipient, notice.TelegramID)
	}
	done, reason := notificationFollowupResult(err)
	completeErr := b.Host.CompleteNotificationFollowup(
		ctx,
		orders.NotificationFollowup{ID: notice.ID, Attempt: notice.DeliveryAttempt, Done: done, Failure: reason},
	)
	return errors.Join(err, completeErr)
}

func notificationText(notice orders.Notification, language string) (string, error) {
	messages := orderMessages{language: language}
	id := i18n.OrderNoticeUpdated
	values := map[string]string{mediaOrderChoice: notice.OrderID}
	switch notice.Kind {
	case "payment_request":
		id = i18n.OrderNoticeReview
	case "accept":
		id = i18n.OrderNoticeAccepted
	case "reject":
		id = i18n.OrderNoticeRejected
	case reminderKind:
		id = i18n.OrderNoticeReminder
	case "capacity":
		id = i18n.OrderNoticeCapacity
		names := make([]string, 0, len(notice.Removed))
		for _, key := range notice.Removed {
			names = append(names, messages.extra(key))
		}
		total, err := notice.Total.MarshalJSON()
		if err != nil {
			return "", err
		}
		values["extras"] = strings.Join(names, ", ")
		values["total"] = messages.number(string(total))
	}
	text := messages.text(id, values)
	return text, messages.err
}

// A known canonical outcome permits follow-up; deferred or rejected work does not.
func (b *Bot) sendPreparedOrderNotice(
	ctx context.Context,
	notice orders.Notification,
) (orders.Notification, bool, error) {
	if notice.FollowupPending {
		return notice, true, nil
	}
	prefs, err := b.API.Preferences(ctx, notice.Recipient)
	if err != nil {
		return notice, false, b.deferOrderNotification(ctx, notice, err)
	}
	text, err := notificationText(notice, prefs.Language)
	if err != nil {
		return notice, false, b.deferOrderNotification(ctx, notice, err)
	}
	gate, err := b.Host.BeginNotification(ctx, delivery.Attempt{ID: notice.ID, Generation: notice.DeliveryAttempt})
	if err != nil || !gate.Ready {
		return notice, false, err
	}
	message, sendErr := b.TG.Send(ctx, telegram.Send{ChatID: notice.TelegramID, Text: text})
	outcome := telegram.DeliveryOutcome(message.ID, sendErr)
	result := orders.NotificationCompletion{ID: notice.ID, Attempt: notice.DeliveryAttempt, Outcome: outcome}
	if outcome.Kind == delivery.Succeeded {
		result.Text = text
	}
	completionCtx, cancelCompletion := deliveryCompletionContext(ctx)
	defer cancelCompletion()
	if err = b.Host.CompleteNotification(completionCtx, result); err != nil {
		return notice, false, err
	}
	if outcome.Kind != delivery.Succeeded {
		return notice, false, nil
	}
	notice.MessageID, notice.DeliveryText = message.ID, text
	return notice, true, nil
}

// DeliverOrderNotification handles one advisory shared-queue candidate.
func (b *Bot) DeliverOrderNotification(ctx context.Context, id int64) error {
	notice, found, err := b.Host.PrepareOrderNotification(ctx, id)
	if err != nil || !found {
		return err
	}
	return b.deliverNotification(ctx, notice)
}

// RecoverOrderNotifications never starts a fresh primary send.
func (b *Bot) RecoverOrderNotifications(ctx context.Context) error {
	notices, err := b.Host.RecoverOrderNotifications(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, notice := range notices {
		failures = errors.Join(failures, b.deliverNotification(ctx, notice))
		if ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}
