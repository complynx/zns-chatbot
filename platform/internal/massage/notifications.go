package massage

import (
	"context"
	"errors"
	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/massage/dbgen"
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
	return result, core.DatabaseOperationError(err)
}

func (s Service) SetPreferences(
	ctx context.Context,
	actor, event string,
	preferences Preferences,
) (Preferences, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Preferences{}, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	prepared, err := s.PreparePreferencesInTx(ctx, tx, actor, event, preferences)
	if err != nil {
		return Preferences{}, err
	}
	result, err := prepared.Apply(ctx)
	if err != nil {
		return Preferences{}, err
	}
	return result, core.DatabaseOperationError(tx.Commit(ctx))
}

// PreparedPreferences holds the current practitioner row until the caller commits.
type PreparedPreferences struct {
	tx           pgx.Tx
	actor, event string
	value        Preferences
}

func (s Service) PreparePreferencesInTx(
	ctx context.Context,
	tx pgx.Tx,
	actor, event string,
	value Preferences,
) (PreparedPreferences, error) {
	p := PreparedPreferences{tx: tx, actor: actor, event: event, value: value}
	var owner string
	err := tx.QueryRow(ctx, `SELECT owner FROM core.massage_specialists WHERE event_id=$1 AND owner=$2 FOR UPDATE`, event, actor).
		Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, problem(http.StatusForbidden, "forbidden")
	}
	return p, core.DatabaseOperationError(err)
}

func (p PreparedPreferences) Apply(ctx context.Context) (Preferences, error) {
	return setPreferences(ctx, p.tx, p.actor, p.event, p.value)
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
		return Preferences{}, core.DatabaseOperationError(err)
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
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()
	rows, err := dbgen.New(tx).
		QueueNotificationReminders(ctx, dbgen.QueueNotificationRemindersParams{BotID: s.Delivery.BotID, Event: event, Now: pgtype.Timestamptz{Time: s.now(), Valid: true}})
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	var pending []delivery.Registration
	for _, row := range rows {
		if err = collectNotificationRegistration(
			ctx,
			tx,
			s.Delivery.BotID,
			row.ID,
			row.DeliveryChat,
			&pending,
		); err != nil {
			return 0, err
		}
	}
	if err = delivery.RegisterBatch(ctx, tx, s.Delivery.BotID, pending); err != nil {
		return 0, err
	}
	return int64(len(rows)), core.DatabaseOperationError(tx.Commit(ctx))
}

type Notice struct {
	DeliveryAttempt int64                     `json:"delivery_attempt"`
	MessageID       int64                     `json:"message_id,omitempty"`
	Wire            *notificationwire.Payload `json:"wire,omitempty"`
	DeliveryText    string                    `json:"delivery_text,omitempty"`
	FollowupPending bool                      `json:"followup_pending,omitempty"`

	ID      int64  `json:"id"`
	Booking string `json:"booking"`
	Kind    string `json:"kind"`
}

// PendingNotices is owner-bound and bounded. The single bot poller drains it.
func (s Service) PendingNotices(ctx context.Context, actor string) ([]Notice, error) {
	rows, err := s.DB.Query(ctx, `SELECT n.id,n.booking_id,n.kind FROM core.massage_notices n
	JOIN core.massage_bookings b ON b.id=n.booking_id
	JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
	WHERE n.owner=$1 AND n.delivery_state='pending' AND (b.cancelled_at IS NULL OR n.kind='cancelled')
	AND (n.kind NOT IN ('booked','cancelled') OR sp.notify_bookings)
	AND (n.kind<>'next' OR sp.notify_next)
	AND (n.kind<>'additional' OR b.starts_at>=$2)
	ORDER BY n.id LIMIT 100`, actor, s.now())
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	notices, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Notice, error) {
		var n Notice
		scanErr := row.Scan(&n.ID, &n.Booking, &n.Kind)
		return n, scanErr
	})
	return notices, core.DatabaseOperationError(err)
}
