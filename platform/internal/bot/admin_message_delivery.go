package bot

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	deliverypolicy "github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

// DeliverAdminMessages claims one destination per poll. The owner schedules
// bounded resends after uncertain outcomes; duplicate messages are possible.
func (b *Bot) DeliverAdminMessages(ctx context.Context) error {
	if err := b.deliverAdminInputExpiry(ctx); err != nil {
		return err
	}
	delivery, found, err := b.Host.ClaimAdminMessage(ctx)
	if err != nil || !found {
		return err
	}
	return b.deliverPreparedAdminMessage(ctx, delivery)
}

func (b *Bot) deliverPreparedAdminMessage(ctx context.Context, delivery adminmessage.Delivery) error {
	if authErr := b.authorizeAdminDelivery(ctx, delivery); authErr != nil {
		return b.finishAdminAuthorizationFailure(ctx, delivery, authErr)
	}
	gate, err := b.Host.BeginAdminMessage(ctx, deliverypolicy.Attempt{ID: delivery.ID, Generation: delivery.Attempt})
	if err != nil {
		return err
	}
	if !gate.Ready {
		return nil
	}
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
	completionCtx, finish := deliveryCompletionContext(ctx)
	defer finish()
	return b.completeAdminDelivery(completionCtx, adminMessageCompletion(delivery, result.ID, sendErr))
}

func (b *Bot) completeAdminDelivery(ctx context.Context, result adminmessage.Completion) error {
	return b.Host.CompleteAdminMessage(ctx, result)
}

func (b *Bot) authorizeAdminDelivery(ctx context.Context, delivery adminmessage.Delivery) error {
	if delivery.Actor == "" || delivery.ActorTelegramID == 0 {
		return identity.ErrZitadelIdentity
	}
	actorCtx, err := b.API.NotificationContext(ctx, delivery.Actor, delivery.ActorTelegramID)
	if err != nil {
		return err
	}
	return b.API.CheckAdminMessagePublication(actorCtx, delivery.Actor, delivery.MessageID)
}

func (b *Bot) finishAdminAuthorizationFailure(
	ctx context.Context,
	delivery adminmessage.Delivery,
	authErr error,
) error {
	if core.IsDatabaseFailure(authErr) {
		return authErr
	}
	denied := errors.Is(authErr, identity.ErrZitadelIdentity) || errors.Is(authErr, identity.ErrZitadelUserInactive)
	if problem, ok := errors.AsType[*core.ProblemError](authErr); ok {
		denied = denied || problem.Status == http.StatusUnauthorized || problem.Status == http.StatusForbidden
		denied = denied || problem.Code == "source_revoked"
	}
	completion := adminmessage.Completion{
		ID:      delivery.ID,
		Attempt: delivery.Attempt,
		Outcome: deliverypolicy.Outcome{
			Kind:    deliverypolicy.Deferred,
			Reason:  "admin_identity_unavailable",
			Missing: true,
		},
	}
	if denied {
		completion.Outcome = deliverypolicy.Outcome{Kind: deliverypolicy.Cancelled, Reason: "admin_identity_denied"}
	}
	if err := b.completeAdminDelivery(ctx, completion); err != nil {
		return err
	}
	if denied {
		return nil
	}
	return authErr
}

const adminSendTimeout = 20 * time.Second

func adminMessageCompletion(item adminmessage.Delivery, messageID int64, sendErr error) adminmessage.Completion {
	return adminmessage.Completion{
		ID:      item.ID,
		Attempt: item.Attempt,
		Outcome: telegram.DeliveryOutcome(messageID, sendErr),
	}
}
