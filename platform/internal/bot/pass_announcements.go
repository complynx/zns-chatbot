package bot

import (
	"context"
	"html"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) deliverRegistrationAnnouncement(ctx context.Context) error {
	item, found, err := b.Host.ClaimRegistrationAnnouncement(ctx)
	if err != nil || !found {
		return err
	}
	return b.deliverPreparedRegistrationAnnouncement(ctx, item)
}

func (b *Bot) deliverPreparedRegistrationAnnouncement(
	ctx context.Context,
	item passbooking.RegistrationAnnouncement,
) error {
	text, err := registrationAnnouncementText(item)
	if err != nil {
		return err
	}
	gate, err := b.Host.BeginRegistrationAnnouncement(ctx, delivery.Attempt{ID: item.ID, Generation: item.Attempts})
	if err != nil {
		return err
	}
	if !gate.Ready {
		return nil
	}
	var chat any = item.Channel
	if numeric, parseErr := strconv.ParseInt(item.Channel, 10, 64); parseErr == nil {
		chat = numeric
	}
	payload := map[string]any{"chat_id": chat, textField: text, "parse_mode": "HTML"}
	if item.ThreadID != nil {
		payload["message_thread_id"] = *item.ThreadID
	}
	sendCtx, cancel := context.WithTimeout(ctx, adminSendTimeout)
	var message telegram.Message
	sendErr := b.TG.Call(sendCtx, "sendMessage", payload, &message)
	cancel()
	completion := announcementCompletion(item.ID, message.ID, sendErr)
	completion.Attempt = item.Attempts
	if completion.Outcome.Reason != "" {
		b.logger().
			WarnContext(ctx, "registration announcement delivery", "announcement", item.ID, "failure", completion.Outcome.Reason)
	}
	completionCtx, finish := deliveryCompletionContext(ctx)
	defer finish()
	return b.Host.CompleteRegistrationAnnouncement(completionCtx, completion)
}

func registrationAnnouncementText(item passbooking.RegistrationAnnouncement) (string, error) {
	// Source thread locales use exact/base then English, not user-locale aliases.
	base, _, _ := strings.Cut(strings.ToLower(item.Locale), "-")
	locale := "en"
	if base == "ru" {
		locale = "ru"
	}
	roleID := i18n.RegistrationAnnouncementFollower
	if item.Role == "leader" {
		roleID = i18n.RegistrationAnnouncementLeader
	}
	role, err := i18n.Translate(locale, roleID, nil)
	if err != nil {
		return "", err
	}
	return i18n.Translate(
		locale,
		i18n.RegistrationAnnouncement,
		map[string]string{profileNameValue: html.EscapeString(item.Name), "role": role},
	)
}

func announcementCompletion(id, messageID int64, err error) passbooking.AnnouncementCompletion {
	return passbooking.AnnouncementCompletion{ID: id, Outcome: telegram.DeliveryOutcome(messageID, err)}
}
