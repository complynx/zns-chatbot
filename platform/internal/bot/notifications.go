package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const reminderKind = "reminder"
const telegramRejected = "telegram_rejected"

func (c APIClient) PendingNotifications(ctx context.Context) ([]orders.Notification, error) {
	var notices []orders.Notification
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodGet, "/internal/notifications", nil, &notices)
	return notices, err
}

func (c APIClient) CompleteNotification(ctx context.Context, id int64, failure string) error {
	body, err := json.Marshal(map[string]string{"failure": failure})
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/notifications/"+strconv.FormatInt(id, 10)+"/complete",
		body,
		&result,
	)
}

func (c APIClient) ClaimReminder(ctx context.Context, id int64) (bool, error) {
	var result struct {
		Claimed bool `json:"claimed"`
	}
	err := c.requestToken(
		ctx,
		c.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/notifications/"+strconv.FormatInt(id, 10)+"/claim-reminder",
		nil,
		&result,
	)
	return result.Claimed, err
}

// DeliverNotifications uses the service queue; user actions still require user authorization.
func (b *Bot) DeliverNotifications(ctx context.Context) error {
	notices, err := b.API.PendingNotifications(ctx)
	if err != nil {
		return err
	}
	for _, notice := range notices {
		attempted, deliveryErr := b.deliverNotification(ctx, notice)
		if deliveryErr == nil {
			continue
		}
		failure := notificationFailure(notice.Kind, attempted, deliveryErr)
		b.logger().WarnContext(ctx, "notification delivery pending", "notification", notice.ID, "error", deliveryErr)
		if retryError := b.API.CompleteNotification(ctx, notice.ID, failure); retryError != nil {
			return retryError
		}
	}
	return nil
}

func notificationFailure(kind string, attempted bool, err error) string {
	if kind == reminderKind && attempted {
		return "reminder_failed"
	}
	if problem, ok := errors.AsType[*telegram.APIError](err); ok {
		// A permanent Telegram rejection while refreshing the card is terminal
		// too, even when the reminder itself has not reached its send claim.
		if kind == reminderKind && (problem.Code == http.StatusForbidden || problem.Code == http.StatusBadRequest) {
			return "reminder_failed"
		}
		switch problem.Code {
		case http.StatusForbidden:
			return "telegram_forbidden"
		case http.StatusBadRequest:
			return telegramRejected
		}
	}
	return notificationRetry
}

const notificationRetry = "telegram_retry"

func (c APIClient) claimNotification(ctx context.Context, notice orders.Notification) (bool, error) {
	if notice.Kind == reminderKind {
		return c.ClaimReminder(ctx, notice.ID)
	}
	return true, nil
}

func (b *Bot) deliverNotification(ctx context.Context, notice orders.Notification) (bool, error) {
	if !notice.Current {
		return false, b.API.CompleteNotification(ctx, notice.ID, "")
	}
	ctx, authErr := b.API.notificationContext(ctx, notice.Recipient, notice.TelegramID)
	if authErr != nil {
		return false, authErr
	}
	var messageID int64
	err := b.DB.QueryRow(ctx, `SELECT message_id FROM bot.notification_deliveries WHERE id=$1`, notice.ID).
		Scan(&messageID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	prefs, err := b.API.Preferences(ctx, notice.Recipient)
	if err != nil {
		return false, err
	}
	text, err := notificationText(notice, prefs.Language)
	if err != nil {
		return false, err
	}
	if messageID == 0 {
		if err = b.RenderOrders(ctx, notice.Recipient, notice.TelegramID); err != nil {
			return false, err
		}
		// Claim only after identity and rendering prerequisites succeed. Once
		// claimed, even an ambiguous Telegram failure is a consumed attempt.
		claimed, claimErr := b.API.claimNotification(ctx, notice)
		if claimErr != nil || !claimed {
			return false, claimErr
		}
		message, sendError := b.TG.Send(ctx, telegram.Send{ChatID: notice.TelegramID, Text: text})
		if sendError != nil {
			return true, sendError
		}
		_, err = b.DB.Exec(
			ctx,
			`INSERT INTO bot.notification_deliveries(id,message_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,
			notice.ID,
			message.ID,
		)
		if err != nil {
			return true, err
		}
	}
	if err = b.record(
		ctx,
		notice.Recipient,
		-notice.ID,
		"order_notification",
		map[string]string{originField: "system", textField: text},
	); err != nil {
		return true, err
	}
	return true, b.API.CompleteNotification(ctx, notice.ID, "")
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
