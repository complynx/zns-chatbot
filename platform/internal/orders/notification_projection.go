package orders

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/orders/dbgen"
)

func (s Service) notificationProjection(
	ctx context.Context,
	q *dbgen.Queries,
	row dbgen.CoreOrderNotification,
) (Notification, error) {
	var notice Notification
	if err := json.Unmarshal(row.Payload, &notice); err != nil {
		return notice, err
	}
	notice.ID, notice.Recipient, notice.TelegramID = row.ID, row.Recipient, row.DeliveryChat
	notice.DeliveryAttempt, notice.MessageID = row.DeliveryAttempt, row.TelegramMessageID
	notice.DeliveryText, notice.FollowupPending = row.DeliveryText, row.FollowupPending
	var err error
	notice.Current, err = q.NotificationCurrent(
		ctx,
		dbgen.NotificationCurrentParams{ID: row.ID, BotID: s.Delivery.BotID},
	)
	return notice, err
}

func (s Service) lockNotificationEligibility(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	q := dbgen.New(tx)
	row, err := q.ReadNotification(ctx, dbgen.ReadNotificationParams{ID: id, BotID: s.Delivery.BotID})
	if err != nil {
		return false, err
	}
	event, err := q.NotificationEvent(ctx, row.OrderID)
	if err != nil {
		return false, err
	}
	if err = LockEvent(ctx, tx, event); err != nil {
		return false, err
	}
	if _, err = q.LockNotificationOrder(ctx, row.OrderID); err != nil {
		return false, err
	}
	recipient, err := q.LockNotificationRecipient(ctx, row.Recipient)
	if err != nil {
		return false, err
	}
	if !recipient.CanBook || recipient.TelegramID <= 0 || recipient.TelegramID != row.DeliveryChat {
		return false, nil
	}
	return q.NotificationCurrent(ctx, dbgen.NotificationCurrentParams{ID: id, BotID: s.Delivery.BotID})
}

// PendingNotifications prepares only lane heads. Wire admission is a separate check.
func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	if err := s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	notices := make([]Notification, 0, notificationPageSize)
	for len(notices) < notificationPageSize {
		row, err := q.PrepareNotification(ctx, dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		notice, err := s.notificationProjection(ctx, q, row)
		if err != nil {
			return nil, err
		}
		notices = append(notices, notice)
	}
	return notices, nil
}
