package massage

import (
	"context"

	"github.com/jackc/pgx/v5"
)

type NoticeRecipient struct {
	Owner      string `json:"owner"`
	TelegramID int64  `json:"telegram_id"`
}

type DeliveryNotice struct {
	Notice      Notice      `json:"notice"`
	Reservation Reservation `json:"reservation"`
	Client      string      `json:"client"`
	Specialist  string      `json:"specialist"`
}

// NoticeRecipients queues due reminders and returns a bounded, fair recipient scan.
// Attempts are recorded only when the adapter reaches that recipient.
func (s Service) NoticeRecipients(ctx context.Context) ([]NoticeRecipient, error) {
	rows, err := s.DB.Query(ctx, `SELECT DISTINCT event_id FROM core.massage_parties
 WHERE starts_at-interval '2 hours'<$1 AND ends_at+interval '2 hours'>$1`, s.now())
	if err != nil {
		return nil, err
	}
	events, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, err
	}
	for _, event := range events {
		if _, err = s.QueueReminders(ctx, event); err != nil {
			return nil, err
		}
	}
	rows, err = s.DB.Query(ctx, `SELECT n.owner,u.telegram_id FROM core.massage_notices n
 JOIN core.users u ON u.id=n.owner JOIN core.massage_bookings b ON b.id=n.booking_id
 JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
 LEFT JOIN core.massage_notification_attempts a ON a.owner=n.owner
 WHERE n.sent_at IS NULL AND (b.cancelled_at IS NULL OR n.kind='cancelled')
 AND (n.kind NOT IN ('booked','cancelled') OR sp.notify_bookings)
 AND (n.kind<>'next' OR sp.notify_next)
 AND (n.kind<>'additional' OR b.starts_at>=$1)
 GROUP BY n.owner,u.telegram_id,a.attempted_at
 ORDER BY a.attempted_at NULLS FIRST,n.owner LIMIT 100`, s.now())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[NoticeRecipient])
}

// DeliveryNotices records rotation before any identity or Telegram work can fail.
// Only the service-authenticated delivery adapter may request this projection.
func (s Service) DeliveryNotices(ctx context.Context, owner string) ([]DeliveryNotice, error) {
	_, err := s.DB.Exec(ctx, `INSERT INTO core.massage_notification_attempts(owner,attempted_at)
 SELECT id,clock_timestamp() FROM core.users WHERE id=$1
 ON CONFLICT(owner) DO UPDATE SET attempted_at=EXCLUDED.attempted_at`, owner)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT n.id,n.booking_id,n.kind,
 b.id,b.starts_at,b.ends_at,b.length,b.price,b.cancelled_at,u.name,sp.name
 FROM core.massage_notices n JOIN core.massage_bookings b ON b.id=n.booking_id
 JOIN core.users u ON u.id=b.owner
 JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
 WHERE n.owner=$1 AND n.sent_at IS NULL AND (b.cancelled_at IS NULL OR n.kind='cancelled')
 AND (n.kind NOT IN ('booked','cancelled') OR sp.notify_bookings)
 AND (n.kind<>'next' OR sp.notify_next)
 AND (n.kind<>'additional' OR b.starts_at>=$2)
 ORDER BY n.id LIMIT 100`, owner, s.now())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeliveryNotice, error) {
		var value DeliveryNotice
		scanErr := row.Scan(&value.Notice.ID, &value.Notice.Booking, &value.Notice.Kind,
			&value.Reservation.ID, &value.Reservation.Start, &value.Reservation.End, &value.Reservation.Length,
			&value.Reservation.Price, &value.Reservation.CancelledAt, &value.Client, &value.Specialist)
		return value, scanErr
	})
}
