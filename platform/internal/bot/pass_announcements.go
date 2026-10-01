package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
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
	if item.Text == "" {
		return errors.New("registration announcement text is not captured")
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
	payload := map[string]any{"chat_id": chat, textField: item.Text, "parse_mode": "HTML"}
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
	return passbooking.RegistrationAnnouncementText(item)
}

func announcementCompletion(id, messageID int64, err error) passbooking.AnnouncementCompletion {
	return passbooking.AnnouncementCompletion{ID: id, Outcome: telegram.DeliveryOutcome(messageID, err)}
}
