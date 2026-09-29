package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

type RegistrationAnnouncement struct {
	ID       int64  `json:"id"`
	Channel  string `json:"channel"`
	ThreadID *int64 `json:"thread_id"`
	Locale   string `json:"locale"`
	Name     string `json:"name"`
	Role     string `json:"role"`
	Attempts int    `json:"attempts"`
}

type AnnouncementCompletion struct {
	ID         int64  `json:"id"`
	MessageID  int64  `json:"message_id"`
	Failure    string `json:"failure"`
	RetryAfter int64  `json:"retry_after"`
}

// Source recalculation considers every registration, including cancelled ones.
// Imported history requires a separate reviewed eligibility decision.
func enqueueRegistrationAnnouncements(ctx context.Context, tx pgx.Tx, eventID string) error {
	_, err := tx.Exec(
		ctx,
		`INSERT INTO core.pass_registration_announcements(event_id,owner,created_at,channel,thread_id,locale,name,role,state)
 SELECT b.event_id,b.owner,b.created_at,e.thread_channel,e.thread_id,e.thread_locale,u.name,b.role,
 CASE WHEN e.thread_channel='' THEN 'suppressed' ELSE 'pending' END
 FROM core.pass_bookings b JOIN core.pass_events e ON e.id=b.event_id JOIN core.users u ON u.id=b.owner
 WHERE b.event_id=$1 AND (e.open_ended OR e.finishes_at>clock_timestamp())
 ON CONFLICT(event_id,owner,created_at) DO NOTHING`,
		eventID,
	)
	return err
}

// ClaimRegistrationAnnouncement marks the send boundary durably. An abandoned claim is uncertain, never
// automatically resent: Telegram has no idempotency key for sendMessage.
func (s Service) ClaimRegistrationAnnouncement(ctx context.Context) (RegistrationAnnouncement, bool, error) {
	var item RegistrationAnnouncement
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return item, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(
		ctx,
		`UPDATE core.pass_registration_announcements SET state='unknown',failure='telegram_outcome_unknown'
 WHERE state='sending' AND available_at<clock_timestamp()-interval '1 minute'`,
	)
	if err != nil {
		return item, false, err
	}
	err = tx.QueryRow(ctx, `WITH next AS (SELECT id FROM core.pass_registration_announcements WHERE state='pending'
 AND available_at<=clock_timestamp() ORDER BY id FOR UPDATE SKIP LOCKED LIMIT 1)
 UPDATE core.pass_registration_announcements a SET state='sending',attempts=attempts+1,available_at=clock_timestamp()
 FROM next WHERE a.id=next.id RETURNING a.id,a.channel,a.thread_id,a.locale,a.name,a.role,a.attempts`).Scan(&item.ID, &item.Channel, &item.ThreadID, &item.Locale, &item.Name, &item.Role, &item.Attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return item, false, tx.Commit(ctx)
	}
	if err != nil {
		return item, false, err
	}
	return item, true, tx.Commit(ctx)
}

func (s Service) CompleteRegistrationAnnouncement(ctx context.Context, c AnnouncementCompletion) error {
	state := "sent"
	switch c.Failure {
	case "":
		if c.MessageID <= 0 {
			return invalid()
		}
	case "telegram_rate_limit":
		if c.RetryAfter < 0 || c.RetryAfter > 3600 {
			return invalid()
		}
		state = "pending"
	case "telegram_rejected":
		state = "failed"
	case "telegram_outcome_unknown":
		state = "unknown"
	default:
		return invalid()
	}
	if c.Failure != "" && c.MessageID != 0 {
		return invalid()
	}
	tag, err := s.DB.Exec(
		ctx,
		`UPDATE core.pass_registration_announcements SET state=CASE WHEN $2='pending' AND attempts>=3 THEN 'failed' ELSE $2 END,
 message_id=$3,failure=$4,available_at=clock_timestamp()+$5::bigint*interval '1 second' WHERE id=$1 AND state='sending'`,
		c.ID,
		state,
		c.MessageID,
		c.Failure,
		c.RetryAfter,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return conflict("pass_announcement_stale")
	}
	return nil
}
