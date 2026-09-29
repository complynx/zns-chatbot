package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (c APIClient) PendingPassNotifications(ctx context.Context) ([]passbooking.Notification, error) {
	var notices []passbooking.Notification
	err := c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodGet, "/internal/pass-notifications", nil, &notices)
	return notices, err
}

func (c APIClient) CompletePassNotification(ctx context.Context, id int64, failure string) error {
	body, err := json.Marshal(map[string]string{"failure": failure})
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return c.requestToken(ctx, c.Signer.DeliveryToken(), http.MethodPost,
		"/internal/pass-notifications/"+strconv.FormatInt(id, 10)+"/complete", body, &result)
}

// DeliverPassNotifications runs under the single poller's session lock.
// Telegram cannot deduplicate a send whose response was lost before persistence.
func (b *Bot) DeliverPassNotifications(ctx context.Context) error {
	if err := b.deliverRegistrationAnnouncement(ctx); err != nil {
		return err
	}
	notices, err := b.API.PendingPassNotifications(ctx)
	if err != nil {
		return err
	}
	for _, notice := range notices {
		if err = b.deliverPassNotification(ctx, notice); err == nil {
			continue
		}
		failure := "telegram_retry"
		if problem, ok := errors.AsType[*telegram.APIError](err); ok {
			switch problem.Code {
			case http.StatusForbidden:
				failure = "telegram_forbidden"
			case http.StatusBadRequest:
				failure = telegramRejected
			}
		}
		b.logger().WarnContext(ctx, "pass notification delivery pending", "notification", notice.ID, "error", err)
		if completeErr := b.API.CompletePassNotification(ctx, notice.ID, failure); completeErr != nil {
			return completeErr
		}
	}
	return nil
}

func (b *Bot) deliverPassNotification(ctx context.Context, notice passbooking.Notification) error {
	if !notice.Current {
		return b.API.CompletePassNotification(ctx, notice.ID, "")
	}
	ctx, authErr := b.API.notificationContext(ctx, notice.Recipient, notice.TelegramID)
	if authErr != nil {
		return authErr
	}
	prefs, err := b.API.Preferences(ctx, notice.Recipient)
	if err != nil {
		return err
	}
	title := notice.Event
	for _, locale := range i18n.FallbackLocales(prefs.Language) {
		if translated := notice.EventTitles[string(locale)]; translated != "" {
			title = translated
			break
		}
	}
	text, err := i18n.Translate(
		prefs.Language,
		i18n.ID("pass.notice."+notice.Kind),
		map[string]string{knowledgeEventQuery: title},
	)
	if err != nil {
		return err
	}
	var messageID int64
	err = b.DB.QueryRow(ctx, `SELECT message_id FROM bot.pass_notification_deliveries WHERE notice_id=$1`, notice.ID).
		Scan(&messageID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if messageID == 0 {
		message, sendErr := b.TG.Send(ctx, telegram.Send{ChatID: notice.TelegramID, Text: text})
		if sendErr != nil {
			return sendErr
		}
		if err = b.storePassNotificationDelivery(ctx, notice, message.ID, text); err != nil {
			return err
		}
	}
	if err = b.refreshPassNotificationViews(ctx, notice); err != nil {
		return err
	}
	return b.API.CompletePassNotification(ctx, notice.ID, "")
}

// Archive before saving the bot receipt. A known delivery always has history;
// a failure before receipt persistence retains the documented resend window.
func (b *Bot) storePassNotificationDelivery(
	ctx context.Context,
	notice passbooking.Notification,
	messageID int64,
	text string,
) error {
	if err := b.API.ArchiveConversation(
		ctx,
		notice.Recipient,
		"pass-notification-"+strconv.FormatInt(notice.ID, 10),
		"system",
		text,
		0,
		false,
	); err != nil {
		return err
	}
	_, err := b.DB.Exec(
		ctx,
		`INSERT INTO bot.pass_notification_deliveries(notice_id,message_id) VALUES($1,$2) ON CONFLICT DO NOTHING`,
		notice.ID,
		messageID,
	)
	return err
}
