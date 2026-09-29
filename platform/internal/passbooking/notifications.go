package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// Notification is delivered using the recipient's current language preference.
// Current is evaluated from the live booking and payment attachment, not just age.
type Notification struct {
	ID          int64             `json:"id"`
	Recipient   string            `json:"recipient"`
	TelegramID  int64             `json:"telegram_id"`
	Event       string            `json:"event"`
	EventTitles map[string]string `json:"event_titles"`
	Owner       string            `json:"owner"`
	Kind        string            `json:"kind"`
	Version     int64             `json:"version"`
	State       string            `json:"state"`
	Price       *int              `json:"price,omitempty"`
	Partner     string            `json:"partner"`
	Attempt     string            `json:"attempt,omitempty"`
	Current     bool              `json:"current"`
}

type noticeSnapshot struct {
	Booking Booking `json:"booking"`
	Attempt string  `json:"attempt,omitempty"`
}

const noticeBatch = 25

// PendingNotifications is a service-only outbox for the existing single bot poller.
// Only the oldest outstanding notification per recipient is returned each time.
func (s Service) PendingNotifications(ctx context.Context) ([]Notification, error) {
	rows, err := s.DB.Query(ctx, `SELECT n.id,n.recipient,u.telegram_id,n.kind,n.payload,n.event_id,n.owner,e.titles
 FROM core.pass_notifications n JOIN core.users u ON u.id=n.recipient JOIN core.pass_events e ON e.id=n.event_id
 WHERE n.delivered_at IS NULL AND n.available_at<=clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM core.pass_notifications older WHERE older.recipient=n.recipient AND older.id<n.id AND older.delivered_at IS NULL)
 ORDER BY n.available_at,n.id LIMIT $1`, noticeBatch)
	if err != nil {
		return nil, err
	}
	type pendingNotice struct {
		notice   Notification
		snapshot noticeSnapshot
	}
	pending, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pendingNotice, error) {
		var item pendingNotice
		scanErr := row.Scan(
			&item.notice.ID,
			&item.notice.Recipient,
			&item.notice.TelegramID,
			&item.notice.Kind,
			&item.snapshot,
			&item.notice.Event,
			&item.notice.Owner,
			&item.notice.EventTitles,
		)
		return item, scanErr
	})
	if err != nil {
		return nil, err
	}
	result := make([]Notification, 0, len(pending))
	for _, item := range pending {
		notice, readErr := s.liveNotification(ctx, item.notice, item.snapshot)
		if readErr != nil {
			return nil, readErr
		}
		result = append(result, notice)
	}
	return result, nil
}

func (s Service) liveNotification(ctx context.Context, notice Notification, old noticeSnapshot) (Notification, error) {
	if notice.Kind == "passport_required" {
		return s.livePassportReminder(ctx, notice)
	}
	if notice.Kind == "payment_request" {
		return s.livePaymentRequest(ctx, notice, old.Attempt)
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT `+bookingColumns+` FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner WHERE b.event_id=$1 AND b.owner=$2`,
		notice.Event,
		notice.Owner,
	)
	if err != nil {
		return notice, err
	}
	b, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Booking])
	if errors.Is(err, pgx.ErrNoRows) {
		return notice, nil
	}
	if err != nil {
		return notice, err
	}
	notice.Version, notice.State, notice.Price, notice.Partner = b.Version, b.State, b.Price, b.Partner
	notice.Attempt = old.Attempt
	notice.Current = currentNotice(notice.Kind, b, old.Booking, notice.TelegramID)
	if notice.Current && (notice.Kind == "invitation" || notice.Kind == "payment_contact_changed") {
		// Later notices supersede earlier ones even when a recipient or contact
		// changes back. Unrelated booking edits do not invalidate the notice.
		err = s.DB.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM core.pass_notifications
 WHERE event_id=$1 AND owner=$2 AND kind=$4 AND id>$3)`, notice.Event, notice.Owner, notice.ID, notice.Kind).
			Scan(&notice.Current)
		if err != nil {
			return notice, err
		}
	}
	if old.Attempt != "" {
		var decision string
		err = s.DB.QueryRow(ctx, `SELECT p.decision
  FROM core.pass_bookings b JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt
  WHERE b.event_id=$1 AND b.owner=$2 AND p.id=$3`, b.Event, b.Owner, old.Attempt).
			Scan(&decision)
		if errors.Is(err, pgx.ErrNoRows) {
			notice.Current = false
			return notice, nil
		}
		if err != nil {
			return notice, err
		}
		notice.Current = paymentNoticeCurrent(notice.Kind, b.State, decision)
	}
	return notice, nil
}

// A shared receipt can remain reviewable after its original submitter detaches.
func (s Service) livePaymentRequest(ctx context.Context, notice Notification, attempt string) (Notification, error) {
	notice.Attempt = attempt
	rows, err := s.DB.Query(ctx, `SELECT `+bookingColumns+`
 FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
 JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt AND p.event_id=b.event_id
 WHERE b.event_id=$1 AND p.id=$2 AND b.state='paid' AND p.decision='pending'
 AND p.receiving_admin=$3
 AND EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=b.event_id AND a.owner=$3)
 ORDER BY u.telegram_id LIMIT 1`, notice.Event, attempt, notice.Recipient)
	if err != nil {
		return notice, err
	}
	b, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Booking])
	if errors.Is(err, pgx.ErrNoRows) {
		return notice, nil
	}
	if err != nil {
		return notice, err
	}
	notice.Owner, notice.Version, notice.State, notice.Price, notice.Partner = b.Owner, b.Version, b.State, b.Price, b.Partner
	notice.Current = true
	return notice, nil
}

func currentNotice(kind string, b, old Booking, recipient int64) bool {
	sameRegistration := b.CreatedAt.Equal(old.CreatedAt)
	sameAssignment := b.AssignedAt != nil && old.AssignedAt != nil && b.AssignedAt.Equal(*old.AssignedAt)
	switch kind {
	case "payment_contact_changed":
		return sameRegistration && b.PaymentAdmin == old.PaymentAdmin
	case "registered":
		return sameRegistration && b.State != cancelled
	case "invitation":
		return sameRegistration && b.State == pending && b.InvitationTarget == recipient
	case "pair_accepted":
		return sameRegistration && b.Partner != "" && b.Partner == old.Partner
	case "pair_declined", "pair_changed", "invitation_expired":
		return sameRegistration && b.Partner == "" && b.State != pending && b.State != cancelled
	case "assigned", "reminder_first", "reminder_second":
		return sameAssignment && b.State == assigned
	case "free_assigned":
		return sameAssignment && b.State == paid && b.Price != nil && *b.Price == 0
	case "waitlisted":
		return sameRegistration && b.State == waitlist
	case "cancelled", "deadline_cancelled":
		return b.Version == old.Version && b.State == cancelled
	default:
		return false
	}
}

func paymentNoticeCurrent(kind, state, decision string) bool {
	switch kind {
	case "payment_accepted":
		return state == paid && decision == paymentAccepted
	case "payment_rejected":
		return state == assigned && decision == "rejected"
	default:
		return false
	}
}

// CompleteNotification accepts only delivery outcomes, never arbitrary database text.
func (s Service) CompleteNotification(ctx context.Context, id int64, failure string) error {
	if id <= 0 ||
		(failure != "" && failure != "telegram_retry" && failure != "telegram_forbidden" && failure != "telegram_rejected") {
		return invalid()
	}
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_notifications WHERE id=$1)`, id).
		Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return &core.ProblemError{Status: http.StatusNotFound, Code: "notification_not_found"}
	}
	if failure == "telegram_retry" {
		_, err := s.DB.Exec(
			ctx,
			`UPDATE core.pass_notifications SET available_at=clock_timestamp()+interval '5 seconds' WHERE id=$1 AND delivered_at IS NULL`,
			id,
		)
		return err
	}
	_, err := s.DB.Exec(
		ctx,
		`UPDATE core.pass_notifications SET delivered_at=clock_timestamp(),failure=$2 WHERE id=$1 AND delivered_at IS NULL`,
		id,
		failure,
	)
	return err
}

func enqueuePassNotice(ctx context.Context, tx pgx.Tx, b *Booking, recipient, kind, generation, attempt string) error {
	payload, err := json.Marshal(noticeSnapshot{Booking: *b, Attempt: attempt})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO core.pass_notifications(event_id,owner,recipient,kind,generation,payload)
 VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, b.Event, b.Owner, recipient, kind, generation, payload)
	return err
}

func noticeVersion(b *Booking) string { return strconv.FormatInt(b.Version, 10) }
func noticeTime(at time.Time) string  { return at.UTC().Format(time.RFC3339Nano) }
