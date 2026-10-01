package passbooking

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/complynx/zns-chatbot/platform/internal/notificationwire"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"

	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

// Notification is delivered using the recipient's current language preference.
// Current is evaluated from the live booking and payment attachment, not just age.
type Notification struct {
	DeliveryAttempt int64                     `json:"delivery_attempt"`
	MessageID       int64                     `json:"message_id,omitempty"`
	Wire            *notificationwire.Payload `json:"wire,omitempty"`
	DeliveryText    string                    `json:"delivery_text,omitempty"`
	FollowupPending bool                      `json:"followup_pending,omitempty"`

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

func (s Service) liveNotification(
	ctx context.Context,
	reader dbgen.DBTX,
	notice Notification,
	old noticeSnapshot,
) (Notification, error) {
	if notice.Kind == "passport_required" {
		return s.livePassportReminder(ctx, reader, notice)
	}
	if notice.Kind == "payment_request" {
		return s.livePaymentRequest(ctx, reader, notice, old.Attempt)
	}
	rows, err := reader.Query(
		ctx,
		`SELECT `+bookingColumns+` FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner WHERE b.event_id=$1 AND b.owner=$2`,
		notice.Event,
		notice.Owner,
	)
	if err != nil {
		return notice, core.DatabaseOperationError(err)
	}
	b, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Booking])
	if errors.Is(err, pgx.ErrNoRows) {
		return notice, nil
	}
	if err != nil {
		return notice, core.DatabaseOperationError(err)
	}
	notice.Version, notice.State, notice.Price, notice.Partner = b.Version, b.State, b.Price, b.Partner
	notice.Attempt = old.Attempt
	notice.Current = currentNotice(notice.Kind, b, old.Booking, notice.TelegramID)
	if notice.Current && (notice.Kind == "invitation" || notice.Kind == "payment_contact_changed") {
		// Later notices supersede earlier ones even when a recipient or contact
		// changes back. Unrelated booking edits do not invalidate the notice.
		err = reader.QueryRow(ctx, `SELECT NOT EXISTS(SELECT 1 FROM core.pass_notifications
 WHERE event_id=$1 AND owner=$2 AND kind=$4 AND id>$3)`, notice.Event, notice.Owner, notice.ID, notice.Kind).
			Scan(&notice.Current)
		if err != nil {
			return notice, core.DatabaseOperationError(err)
		}
	}
	if old.Attempt != "" {
		var decision string
		err = reader.QueryRow(ctx, `SELECT p.decision
  FROM core.pass_bookings b JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt
  WHERE b.event_id=$1 AND b.owner=$2 AND p.id=$3`, b.Event, b.Owner, old.Attempt).
			Scan(&decision)
		if errors.Is(err, pgx.ErrNoRows) {
			notice.Current = false
			return notice, nil
		}
		if err != nil {
			return notice, core.DatabaseOperationError(err)
		}
		notice.Current = paymentNoticeCurrent(notice.Kind, b.State, decision)
	}
	return notice, nil
}

// A shared receipt can remain reviewable after its original submitter detaches.
func (s Service) livePaymentRequest(
	ctx context.Context,
	reader dbgen.DBTX,
	notice Notification,
	attempt string,
) (Notification, error) {
	notice.Attempt = attempt
	rows, err := reader.Query(ctx, `SELECT `+bookingColumns+`
 FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
 JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt AND p.event_id=b.event_id
 WHERE b.event_id=$1 AND p.id=$2 AND b.state='paid' AND p.decision='pending'
 AND p.receiving_admin=$3
 AND EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=b.event_id AND a.owner=$3)
 ORDER BY u.telegram_id LIMIT 1`, notice.Event, attempt, notice.Recipient)
	if err != nil {
		return notice, core.DatabaseOperationError(err)
	}
	b, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Booking])
	if errors.Is(err, pgx.ErrNoRows) {
		return notice, nil
	}
	if err != nil {
		return notice, core.DatabaseOperationError(err)
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
		return state == assigned && decision == paymentRejected
	default:
		return false
	}
}

func enqueuePassNotice(
	ctx context.Context,
	tx pgx.Tx,
	botID int64,
	pending *[]delivery.Registration,
	b *Booking,
	recipient, kind, generation, attempt string,
) error {
	payload, err := json.Marshal(noticeSnapshot{Booking: *b, Attempt: attempt})
	if err != nil {
		return err
	}
	row, err := dbgen.New(tx).EnqueueNotification(ctx, dbgen.EnqueueNotificationParams{
		EventID:    b.Event,
		Owner:      b.Owner,
		Recipient:  recipient,
		Kind:       kind,
		Generation: generation,
		Payload:    payload,
		BotID:      botID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return core.DatabaseOperationError(err)
	}
	return collectNotificationRegistration(ctx, tx, botID, row.ID, row.DeliveryChat, pending)
}

func noticeVersion(b *Booking) string { return strconv.FormatInt(b.Version, 10) }
func noticeTime(at time.Time) string  { return at.UTC().Format(time.RFC3339Nano) }
