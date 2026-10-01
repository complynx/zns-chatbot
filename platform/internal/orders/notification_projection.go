package orders

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/complynx/zns-chatbot/platform/internal/core"

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
	wire, wireErr := notificationwire.Decode(row.DeliveryWirePayload)
	if wireErr != nil {
		return notice, wireErr
	}
	notice.Wire = wire
	notice.ID, notice.Recipient, notice.TelegramID = row.ID, row.Recipient, row.DeliveryChat
	notice.DeliveryAttempt, notice.MessageID = row.DeliveryAttempt, row.TelegramMessageID
	notice.DeliveryText, notice.FollowupPending = row.DeliveryText, row.FollowupPending
	var err error
	notice.Current, err = q.NotificationCurrent(
		ctx,
		dbgen.NotificationCurrentParams{ID: row.ID, BotID: s.Delivery.BotID},
	)
	err = core.DatabaseOperationError(err)
	if err == nil && notice.Current && notice.Kind == refundRequest {
		task, readErr := scanRefund(
			s.DB.QueryRow(
				ctx,
				`SELECT `+refundColumns+` FROM core.order_refund_tasks t WHERE t.id=$1`,
				notice.RefundID,
			),
		)
		if readErr != nil {
			return notice, readErr
		}
		task.CanConfirm = task.State == refundPending
		notice.Refund = &task
	}
	return notice, err
}

func (s Service) lockNotificationEligibility(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	q := dbgen.New(tx)
	row, err := q.ReadNotification(ctx, dbgen.ReadNotificationParams{ID: id, BotID: s.Delivery.BotID})
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	event, err := q.NotificationEvent(ctx, row.OrderID)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	var notice Notification
	if err = json.Unmarshal(row.Payload, &notice); err != nil {
		return false, err
	}
	if notice.Kind == refundRequest {
		if err = lockRefundPassEvent(ctx, tx, event); err != nil {
			return false, err
		}
	}
	if err = LockEvent(ctx, tx, event); err != nil {
		return false, err
	}
	if _, err = q.LockNotificationOrder(ctx, row.OrderID); err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if notice.Kind == refundRequest {
		task, readErr := scanRefund(
			tx.QueryRow(
				ctx,
				`SELECT `+refundColumns+` FROM core.order_refund_tasks t WHERE t.id=$1 FOR UPDATE`,
				notice.RefundID,
			),
		)
		if readErr != nil {
			return false, readErr
		}
		if err = lockRefundAmbassador(ctx, tx, task); err != nil {
			return false, err
		}
	}
	recipient, err := q.LockNotificationRecipient(ctx, row.Recipient)
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if !recipient.CanBook || recipient.TelegramID <= 0 || recipient.TelegramID != row.DeliveryChat {
		return false, nil
	}
	current, err := q.NotificationCurrent(ctx, dbgen.NotificationCurrentParams{ID: id, BotID: s.Delivery.BotID})
	return current, core.DatabaseOperationError(err)
}

// PendingNotifications prepares only lane heads. Wire admission is a separate check.
func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.RouteRefunds(ctx); err != nil {
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
