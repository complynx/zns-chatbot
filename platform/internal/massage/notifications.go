package massage

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Preferences struct {
	Bookings bool `json:"bookings"`
	Next     bool `json:"next"`
}

func (s Service) Preferences(ctx context.Context, actor, event string) (Preferences, error) {
	var result Preferences
	err := s.DB.QueryRow(ctx, `SELECT notify_bookings,notify_next FROM core.massage_specialists WHERE event_id=$1 AND owner=$2`, event, actor).
		Scan(&result.Bookings, &result.Next)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, problem(http.StatusForbidden, "forbidden")
	}
	return result, err
}

func (s Service) SetPreferences(
	ctx context.Context,
	actor, event string,
	preferences Preferences,
) (Preferences, error) {
	return setPreferences(ctx, s.DB, actor, event, preferences)
}

type preferenceWriter interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func setPreferences(
	ctx context.Context,
	db preferenceWriter,
	actor, event string,
	preferences Preferences,
) (Preferences, error) {
	result, err := db.Exec(
		ctx,
		`UPDATE core.massage_specialists SET notify_bookings=$3,notify_next=$4 WHERE event_id=$1 AND owner=$2`,
		event,
		actor,
		preferences.Bookings,
		preferences.Next,
	)
	if err != nil {
		return Preferences{}, err
	}
	if result.RowsAffected() != 1 {
		return Preferences{}, problem(http.StatusForbidden, "forbidden")
	}
	return preferences, nil
}

// QueueReminders is a trusted maintenance operation. Unique notice keys make
// retries safe; notices are acknowledged only after the delivery adapter sends.
// Source reminders include missed starts back to the first party's early window.
func (s Service) QueueReminders(ctx context.Context, event string) (int64, error) {
	result, err := s.DB.Exec(ctx, `INSERT INTO core.massage_notices(booking_id,owner,kind)
	SELECT b.id,n.owner,n.kind FROM core.massage_bookings b
	JOIN core.massage_events e ON e.id=b.event_id
	JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
	CROSS JOIN LATERAL (VALUES
	 (b.owner,'prior_long',e.prior_long,true),
	 (b.owner,'prior_short',e.prior_short,true),
	 (b.specialist,'next',interval '5 minutes',sp.notify_next)) n(owner,kind,prior,enabled)
	WHERE b.event_id=$1 AND b.cancelled_at IS NULL AND n.enabled
	AND b.starts_at >= (SELECT min(p.starts_at)-interval '2 hours' FROM core.massage_parties p WHERE p.event_id=b.event_id)
	AND b.starts_at<$2::timestamptz+n.prior
	ON CONFLICT DO NOTHING`, event, s.now())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}

type Notice struct {
	ID      int64  `json:"id"`
	Booking string `json:"booking"`
	Kind    string `json:"kind"`
}

// PendingNotices is owner-bound and bounded. The single bot poller drains it.
func (s Service) PendingNotices(ctx context.Context, actor string) ([]Notice, error) {
	rows, err := s.DB.Query(ctx, `SELECT n.id,n.booking_id,n.kind FROM core.massage_notices n
	JOIN core.massage_bookings b ON b.id=n.booking_id
	JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
	WHERE n.owner=$1 AND n.sent_at IS NULL AND (b.cancelled_at IS NULL OR n.kind='cancelled')
	AND (n.kind NOT IN ('booked','cancelled') OR sp.notify_bookings)
	AND (n.kind<>'next' OR sp.notify_next)
	AND (n.kind<>'additional' OR b.starts_at>=$2)
	ORDER BY n.id LIMIT 100`, actor, s.now())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Notice])
}

func (s Service) AcknowledgeNotice(ctx context.Context, actor string, id int64) error {
	result, err := s.DB.Exec(
		ctx,
		`UPDATE core.massage_notices SET sent_at=COALESCE(sent_at,now()) WHERE owner=$1 AND id=$2`,
		actor,
		id,
	)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return problem(http.StatusNotFound, "not_found")
	}
	return nil
}
