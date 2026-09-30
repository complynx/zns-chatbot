package legacyfood

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood/dbgen"
)

const notificationBatchSize = 25

func (s Service) notificationProjection(
	ctx context.Context,
	q *dbgen.Queries,
	row dbgen.CoreFoodNotification,
) (Notification, error) {
	notice := Notification{
		ID:              row.ID,
		EventID:         row.EventID,
		Owner:           row.Owner,
		TelegramID:      row.DeliveryChat,
		Kind:            row.Kind,
		Payload:         row.Payload,
		DeliveryAttempt: row.DeliveryAttempt,
		MessageID:       row.TelegramMessageID,
		DeliveryText:    row.DeliveryText,
		FollowupPending: row.FollowupPending,
	}
	var err error
	notice.Current, err = q.NotificationCurrent(
		ctx,
		dbgen.NotificationCurrentParams{EventBotID: s.BotID, ID: row.ID, BotID: s.Delivery.BotID},
	)
	return notice, core.DatabaseOperationError(err)
}

// lockNotificationEligibility sanitizes each statement; pgx.ErrNoRows stays
// visible so callers keep mapping a missing row to a stale attempt.
func (s Service) lockNotificationEligibility(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	q := dbgen.New(tx)
	row, err := q.ReadNotification(ctx, dbgen.ReadNotificationParams{ID: id, BotID: s.Delivery.BotID})
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if _, err = q.LockNotificationEvent(
		ctx,
		dbgen.LockNotificationEventParams{Event: row.EventID, EventBotID: s.BotID},
	); err != nil {
		return false, core.DatabaseOperationError(err)
	}
	recipient, err := q.LockNotificationRecipient(ctx, row.Owner)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if !recipient.CanBook || recipient.TelegramID <= 0 || recipient.TelegramID != row.DeliveryChat {
		return false, nil
	}
	current, err := q.NotificationCurrent(
		ctx,
		dbgen.NotificationCurrentParams{EventBotID: s.BotID, ID: id, BotID: s.Delivery.BotID},
	)
	return current, core.DatabaseOperationError(err)
}

// PendingNotifications leases only lane heads; sending requires fresh admission.
func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	if err := s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	notices := make([]Notification, 0, notificationBatchSize)
	for len(notices) < notificationBatchSize {
		row, err := q.PrepareNotification(ctx, dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, core.DatabaseOperationError(err)
		}
		notice, err := s.notificationProjection(ctx, q, row)
		if err != nil {
			return nil, err
		}
		notices = append(notices, notice)
	}
	return notices, nil
}
