package bot

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) deliverRegistrationAnnouncement(ctx context.Context) error {
	var claim struct {
		Announcement passbooking.RegistrationAnnouncement `json:"announcement"`
		Found        bool                                 `json:"found"`
	}
	err := b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-announcements/claim",
		nil,
		&claim,
	)
	if err != nil || !claim.Found {
		return err
	}
	item := claim.Announcement
	text, err := registrationAnnouncementText(item)
	if err != nil {
		return err
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
	if completion.Failure != "" {
		b.logger().
			WarnContext(ctx, "registration announcement delivery", "announcement", item.ID, "failure", completion.Failure)
	}
	body, err := json.Marshal(completion)
	if err != nil {
		return err
	}
	var result struct {
		OK bool `json:"ok"`
	}
	return b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/pass-announcements/complete",
		body,
		&result,
	)
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
	result := passbooking.AnnouncementCompletion{ID: id, MessageID: messageID}
	if err == nil && messageID > 0 {
		return result
	}
	result.MessageID = 0
	result.Failure = "telegram_outcome_unknown"
	if apiErr, ok := errors.AsType[*telegram.APIError](err); ok {
		result.Failure = telegramRejected
		if apiErr.Code == http.StatusTooManyRequests && apiErr.Parameters.RetryAfter >= 0 &&
			apiErr.Parameters.RetryAfter <= 3600 {
			result.Failure = "telegram_rate_limit"
			result.RetryAfter = apiErr.Parameters.RetryAfter
		}
	}
	return result
}
