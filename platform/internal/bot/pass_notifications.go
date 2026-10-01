package bot

import (
	"context"
	"errors"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/complynx/zns-chatbot/platform/internal/bot/dbgen"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func (b *Bot) passNotificationWire(
	ctx context.Context,
	notice passbooking.Notification,
) (*notificationwire.Payload, error) {
	if notice.Wire != nil {
		return notice.Wire, nil
	}
	text, err := b.passNotificationText(ctx, notice)
	if err != nil {
		return nil, err
	}
	return notificationWireCandidate(telegram.Send{ChatID: notice.TelegramID, Text: text})
}

// DeliverPassNotifications preserves each canonical outcome before follow-up work.
func (b *Bot) DeliverPassNotifications(ctx context.Context) error {
	failures := b.deliverRegistrationAnnouncement(ctx)
	if core.IsDatabaseFailure(failures) || ctx.Err() != nil {
		return errors.Join(deliveryFailure(failures), ctx.Err())
	}
	notices, err := b.Host.PendingPassNotifications(ctx)
	if err != nil {
		return errors.Join(failures, err)
	}
	for _, notice := range notices {
		err = b.deliverPassNotification(ctx, notice)
		failures = errors.Join(failures, deliveryFailure(err))
		if core.IsDatabaseFailure(err) || ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}

func (b *Bot) deferPassNotification(ctx context.Context, n passbooking.Notification, err error) error {
	if core.IsDatabaseFailure(err) {
		return err
	}
	if n.FollowupPending {
		done, reason := notificationFollowupResult(err)
		return b.Host.CompletePassNotificationFollowup(
			ctx,
			passbooking.NotificationFollowup{ID: n.ID, Attempt: n.DeliveryAttempt, Done: done, Failure: reason},
		)
	}
	return b.Host.CompletePassNotification(
		ctx,
		passbooking.NotificationCompletion{
			ID:      n.ID,
			Attempt: n.DeliveryAttempt,
			Outcome: notificationPreflightOutcome(err),
		},
	)
}

func (b *Bot) deliverPassNotification(ctx context.Context, notice passbooking.Notification) error {
	if !notice.Current {
		if notice.FollowupPending {
			return b.Host.CompletePassNotificationFollowup(
				ctx,
				passbooking.NotificationFollowup{
					ID:      notice.ID,
					Attempt: notice.DeliveryAttempt,
					Done:    true,
					Failure: notificationNoLongerCurrent,
				},
			)
		}
		return b.Host.CompletePassNotification(
			ctx,
			passbooking.NotificationCompletion{
				ID:      notice.ID,
				Attempt: notice.DeliveryAttempt,
				Outcome: delivery.Outcome{Kind: delivery.Cancelled, Reason: notificationNoLongerCurrent},
			},
		)
	}
	authenticated, err := b.API.NotificationContext(ctx, notice.Recipient, notice.TelegramID)
	if err != nil {
		return b.deferPassNotification(ctx, notice, err)
	}
	ctx = authenticated
	var known bool
	notice, known, err = b.sendPreparedPassNotice(ctx, notice)
	if err != nil || !known {
		return err
	}
	ctx = withBotDeliveryOrigin(
		ctx,
		delivery.Reference{Owner: delivery.Passes, Key: strconv.FormatInt(notice.ID, 10), Effect: botRefreshEffect},
	)
	err = b.storePassNotificationDelivery(ctx, notice, notice.MessageID, notice.DeliveryText)
	if err == nil {
		err = b.refreshPassNotificationViews(ctx, notice)
	}
	if core.IsDatabaseFailure(err) {
		return err
	}
	done, reason := notificationFollowupResult(err)
	completeErr := b.Host.CompletePassNotificationFollowup(
		ctx,
		passbooking.NotificationFollowup{ID: notice.ID, Attempt: notice.DeliveryAttempt, Done: done, Failure: reason},
	)
	return errors.Join(err, completeErr)
}

func (b *Bot) passNotificationText(ctx context.Context, notice passbooking.Notification) (string, error) {
	prefs, err := b.API.Preferences(ctx, notice.Recipient)
	if err != nil {
		return "", err
	}
	title := notice.Event
	for _, locale := range i18n.FallbackLocales(prefs.Language) {
		if translated := notice.EventTitles[string(locale)]; translated != "" {
			title = translated
			break
		}
	}
	return i18n.Translate(
		prefs.Language,
		i18n.ID("pass.notice."+notice.Kind),
		map[string]string{knowledgeEventQuery: title},
	)
}

// The domain receipt already owns transport success; this follow-up never sends.
func (b *Bot) storePassNotificationDelivery(
	ctx context.Context,
	notice passbooking.Notification,
	messageID int64,
	text string,
) error {
	if err := b.Host.ArchiveOutcome(
		ctx,
		notice.Recipient,
		"pass-notification-"+strconv.FormatInt(notice.ID, 10),
		text,
	); err != nil {
		return err
	}
	err := dbgen.New(b.DB).
		StorePassNotificationReceipt(ctx, dbgen.StorePassNotificationReceiptParams{ID: notice.ID, MessageID: messageID})
	return core.DatabaseOperationError(err)
}

// A known canonical outcome permits follow-up; deferred or rejected work does not.
func (b *Bot) sendPreparedPassNotice(
	ctx context.Context,
	notice passbooking.Notification,
) (passbooking.Notification, bool, error) {
	if notice.FollowupPending {
		return notice, true, nil
	}
	wire, err := b.passNotificationWire(ctx, notice)
	if err != nil {
		return notice, false, b.deferPassNotification(ctx, notice, err)
	}
	gate, err := b.Host.BeginPassNotification(
		ctx,
		passbooking.NotificationAttempt{
			Attempt: {ID: notice.ID, Generation: notice.DeliveryAttempt},
			Wire:    wire,
		},
	)
	if err != nil || !gate.Ready {
		return notice, false, err
	}
	outcome, text, err := b.sendNotificationWire(ctx, notice.TelegramID, gate.Wire)
	if err != nil {
		return notice, false, err
	}
	result := passbooking.NotificationCompletion{ID: notice.ID, Attempt: notice.DeliveryAttempt, Outcome: outcome}
	result.Text = text
	completionCtx, cancelCompletion := deliveryCompletionContext(ctx)
	defer cancelCompletion()
	if err = b.Host.CompletePassNotification(completionCtx, result); err != nil {
		return notice, false, err
	}
	if outcome.Kind != delivery.Succeeded {
		return notice, false, nil
	}
	notice.MessageID, notice.DeliveryText = outcome.MessageID, text
	return notice, true, nil
}

// DeliverPassNotification handles one advisory shared-queue candidate.
func (b *Bot) DeliverPassNotification(ctx context.Context, id int64) error {
	notice, found, err := b.Host.PreparePassNotification(ctx, id)
	if err != nil || !found {
		return err
	}
	return b.deliverPassNotification(ctx, notice)
}

// RecoverPassNotifications never starts a fresh primary send.
func (b *Bot) RecoverPassNotifications(ctx context.Context) error {
	notices, err := b.Host.RecoverPassNotifications(ctx)
	if err != nil {
		return err
	}
	var failures error
	for _, notice := range notices {
		err = b.deliverPassNotification(ctx, notice)
		failures = errors.Join(failures, deliveryFailure(err))
		if core.IsDatabaseFailure(err) || ctx.Err() != nil {
			return errors.Join(failures, ctx.Err())
		}
	}
	return failures
}
