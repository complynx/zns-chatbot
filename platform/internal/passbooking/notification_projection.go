package passbooking

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

func (s Service) notificationProjection(
	ctx context.Context,
	reader dbgen.DBTX,
	row dbgen.CorePassNotification,
) (Notification, error) {
	notice := Notification{ID: row.ID, Recipient: row.Recipient, TelegramID: row.DeliveryChat, Event: row.EventID,
		Owner: row.Owner, Kind: row.Kind, DeliveryAttempt: row.DeliveryAttempt, MessageID: row.TelegramMessageID,
		DeliveryText: row.DeliveryText, FollowupPending: row.FollowupPending}
	var captured []byte
	if row.LastUncertainAttempt.Valid || row.DeliveryState == "sent" {
		captured = row.DeliveryWirePayload
	}
	wire, present, wireErr := notificationwire.Decode(captured)
	if wireErr != nil {
		return notice, wireErr
	}
	if present {
		notice.Wire = &wire
	}
	var snapshot noticeSnapshot
	if err := json.Unmarshal(row.Payload, &snapshot); err != nil {
		return notice, err
	}
	titles, err := dbgen.New(reader).NotificationTitles(ctx, row.EventID)
	if err != nil {
		return notice, core.DatabaseOperationError(err)
	}
	if err = json.Unmarshal(titles, &notice.EventTitles); err != nil {
		return notice, err
	}
	recipient, err := dbgen.New(reader).LockNotificationRecipient(ctx, row.Recipient)
	if err != nil {
		return notice, core.DatabaseOperationError(err)
	}
	notice, err = s.liveNotification(ctx, reader, notice, snapshot)
	notice.Current = notice.Current && recipient.CanBook && recipient.TelegramID > 0 &&
		recipient.TelegramID == row.DeliveryChat
	return notice, err
}

func (s Service) lockNotificationEligibility(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	q := dbgen.New(tx)
	row, err := q.ReadNotification(ctx, dbgen.ReadNotificationParams{ID: id, BotID: s.Delivery.BotID})
	if err != nil {
		return false, core.DatabaseOperationError(err)
	}
	if _, err = q.LockNotificationEvent(ctx, row.EventID); err != nil {
		return false, core.DatabaseOperationError(err)
	}
	notice, err := s.notificationProjection(ctx, tx, row)
	return notice.Current, err
}

// PendingNotifications prepares one eligible lane head per recipient for the adapter.
func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	if err := s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	notices := make([]Notification, 0, noticeBatch)
	for len(notices) < noticeBatch {
		row, err := q.PrepareNotification(ctx, dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, core.DatabaseOperationError(err)
		}
		notice, err := s.notificationProjection(ctx, s.DB, row)
		if err != nil {
			return nil, err
		}
		notices = append(notices, notice)
	}
	return notices, nil
}
