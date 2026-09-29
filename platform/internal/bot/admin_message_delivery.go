package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// DeliverAdminMessages claims one destination per poll. Ambiguous network errors
// are terminal and visible; retrying them automatically risks duplicate sends.
func (b *Bot) DeliverAdminMessages(ctx context.Context) error {
	if err := b.deliverAdminInputExpiry(ctx); err != nil {
		return err
	}
	var claim struct {
		Delivery adminmessage.Delivery `json:"delivery"`
		Found    bool                  `json:"found"`
	}
	err := b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/claim",
		nil,
		&claim,
	)
	if err != nil || !claim.Found {
		return err
	}
	delivery := claim.Delivery
	var chat any = delivery.Destination.Chat
	if numeric, parseErr := strconv.ParseInt(delivery.Destination.Chat, 10, 64); parseErr == nil {
		chat = numeric
	}
	payload := map[string]any{broadcastChatKey: chat}
	if delivery.Destination.Thread > 0 {
		payload["message_thread_id"] = delivery.Destination.Thread
	}
	method := "sendMessage"
	if delivery.Content.FromMessage > 0 {
		method = "forwardMessage"
		payload["from_chat_id"] = delivery.Content.FromChat
		payload["message_id"] = delivery.Content.FromMessage
	} else {
		payload["text"] = delivery.Content.Text
		if delivery.Content.ParseMode != "" {
			payload["parse_mode"] = delivery.Content.ParseMode
		}
	}
	sendCtx, cancel := context.WithTimeout(ctx, adminSendTimeout)
	var result telegram.Message
	sendErr := b.TG.Call(sendCtx, method, payload, &result)
	cancel()
	body, err := json.Marshal(adminMessageCompletion(delivery, result.ID, sendErr))
	if err != nil {
		return err
	}
	var completed struct {
		OK bool `json:"ok"`
	}
	return b.API.requestToken(
		ctx,
		b.API.Signer.DeliveryToken(),
		http.MethodPost,
		"/internal/admin-messages/complete",
		body,
		&completed,
	)
}

const (
	adminSendTimeout  = 20 * time.Second
	adminSendAttempts = 3
)

func adminMessageCompletion(delivery adminmessage.Delivery, messageID int64, sendErr error) adminmessage.Completion {
	result := adminmessage.Completion{ID: delivery.ID, Attempt: delivery.Attempt, MessageID: messageID}
	if sendErr == nil && messageID > 0 {
		return result
	}
	result.MessageID = 0
	result.Failure = "telegram_outcome_unknown"
	apiErr, ok := errors.AsType[*telegram.APIError](sendErr)
	if !ok {
		return result
	}
	result.Failure = "admin_telegram_rejected"
	if apiErr.Code != http.StatusTooManyRequests {
		return result
	}
	result.Failure = "telegram_rate_limit"
	delay := apiErr.Parameters.RetryAfter
	if delay < 0 || delay > adminmessage.MaxRetryAfterSeconds {
		result.Failure = "telegram_invalid_cooldown"
		return result
	}
	result.Retry = delivery.Attempt < adminSendAttempts
	if result.Retry {
		result.RetryAfter = delay
	}
	return result
}
