package massage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/massage/dbgen"
)

const notificationBatchSize = 25

type NoticeRecipient struct {
	Owner      string `json:"owner"`
	TelegramID int64  `json:"telegram_id"`
}
type DeliveryNotice struct {
	Owner       string      `json:"owner"`
	TelegramID  int64       `json:"telegram_id"`
	Notice      Notice      `json:"notice"`
	Reservation Reservation `json:"reservation"`
	Client      string      `json:"client"`
	Specialist  string      `json:"specialist"`
	Current     bool        `json:"current"`
}

// NoticeRecipients keeps recipient rotation separate from transport admission.

func (s Service) NoticeRecipients(ctx context.Context) ([]NoticeRecipient, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	events, err := q.NotificationReminderEvents(ctx, pgtype.Timestamptz{Time: s.now(), Valid: true})
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if _, err = s.QueueReminders(ctx, event); err != nil {
			return nil, err
		}
	}
	if err = s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	rows, err := q.NotificationRecipients(ctx, s.Delivery.BotID)
	if err != nil {
		return nil, err
	}
	values := make([]NoticeRecipient, 0, len(rows))
	for _, row := range rows {
		values = append(values, NoticeRecipient{Owner: row.Owner, TelegramID: row.TelegramID})
	}
	return values, nil
}

// DeliveryNotices prepares lane heads for an authenticated delivery adapter.
func (s Service) DeliveryNotices(ctx context.Context, owner string) ([]DeliveryNotice, error) {
	if err := s.Delivery.Validate(); err != nil {
		return nil, err
	}
	q := dbgen.New(s.DB)
	if err := q.RecordNotificationRotation(ctx, owner); err != nil {
		return nil, err
	}
	if err := s.recoverNotificationSends(ctx); err != nil {
		return nil, err
	}
	values := make([]DeliveryNotice, 0, notificationBatchSize)
	for len(values) < notificationBatchSize {
		row, err := q.PrepareNotification(ctx, dbgen.PrepareNotificationParams{BotID: s.Delivery.BotID, Owner: owner})
		if errors.Is(err, pgx.ErrNoRows) {
			break
		}
		if err != nil {
			return nil, err
		}
		value, err := s.notificationProjection(ctx, q, row)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (s Service) notificationProjection(
	ctx context.Context,
	q *dbgen.Queries,
	row dbgen.CoreMassageNotice,
) (DeliveryNotice, error) {
	value := DeliveryNotice{
		Owner:      row.Owner,
		TelegramID: row.DeliveryChat,
		Notice: Notice{ID: row.ID, Booking: row.BookingID, Kind: row.Kind, DeliveryAttempt: row.DeliveryAttempt,
			MessageID: row.TelegramMessageID, DeliveryText: row.DeliveryText, FollowupPending: row.FollowupPending},
	}
	booking, err := q.NotificationBookingProjection(ctx, row.BookingID)
	if err != nil {
		return value, err
	}
	value.Reservation = Reservation{
		ID:     booking.ID,
		Start:  booking.StartsAt.Time,
		End:    booking.EndsAt.Time,
		Length: int(booking.Length),
		Price:  int(booking.Price),
	}
	if booking.CancelledAt.Valid {
		value.Reservation.CancelledAt = &booking.CancelledAt.Time
	}
	value.Client, value.Specialist = booking.Client, booking.Specialist
	value.Current, err = q.NotificationCurrent(
		ctx,
		dbgen.NotificationCurrentParams{
			ID:    row.ID,
			BotID: s.Delivery.BotID,
			Now:   pgtype.Timestamptz{Time: s.now(), Valid: true},
		},
	)
	return value, err
}

func (s Service) lockNotificationEligibility(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	q := dbgen.New(tx)
	row, err := q.ReadNotification(ctx, dbgen.ReadNotificationParams{ID: id, BotID: s.Delivery.BotID})
	if err != nil {
		return false, err
	}
	if _, err = q.LockNotificationBooking(ctx, row.BookingID); err != nil {
		return false, err
	}
	recipient, err := q.LockNotificationRecipient(ctx, row.Owner)
	if err != nil {
		return false, err
	}
	if !recipient.CanBook || recipient.TelegramID <= 0 || recipient.TelegramID != row.DeliveryChat {
		return false, nil
	}
	return q.NotificationCurrent(
		ctx,
		dbgen.NotificationCurrentParams{
			ID:    id,
			BotID: s.Delivery.BotID,
			Now:   pgtype.Timestamptz{Time: s.now(), Valid: true},
		},
	)
}
