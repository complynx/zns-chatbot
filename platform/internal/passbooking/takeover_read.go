package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// TakeoverTarget separates the current contact from recorded receiving provenance.
type TakeoverTarget struct {
	Booking         Booking           `json:"booking"`
	Name            string            `json:"name"`
	ActorVersion    int64             `json:"actor_version"`
	ReceivingAdmin  string            `json:"receiving_admin"`
	CanBackfill     bool              `json:"can_backfill"`
	PaymentContact  *Contact          `json:"payment_contact,omitempty"`
	ReceiverContact *Contact          `json:"receiver_contact,omitempty"`
	EventTitles     map[string]string `json:"event_titles"`
}

// TakeoverTarget requires current global or event payment-administrator rights.
func (s Service) TakeoverTarget(ctx context.Context, actor, eventID string, telegramID int64) (TakeoverTarget, error) {
	var result TakeoverTarget
	if telegramID <= 0 {
		return result, invalid()
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = readEvent(ctx, tx, eventID); err != nil {
		return result, err
	}
	if _, err = authorize(ctx, tx, actor, CommandTakeover, eventID); err != nil {
		return result, err
	}
	var owner string
	err = tx.QueryRow(ctx, `SELECT id,name FROM core.users WHERE telegram_id=$1`, telegramID).Scan(&owner, &result.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, forbidden()
	}
	if err != nil {
		return result, err
	}
	records, err := readBookings(ctx, tx, eventID)
	if err != nil {
		return result, err
	}
	b := records[owner]
	if b == nil {
		return result, conflict("pass_booking_missing")
	}
	result.Booking, result.ActorVersion = *b, bookingVersion(records[actor])
	err = tx.QueryRow(ctx, `SELECT COALESCE(legacy.receiving_admin,p.receiving_admin,f.legacy_receiving_admin,'')
FROM core.pass_bookings b LEFT JOIN core.pass_payment_attempts p ON p.id=b.payment_attempt
LEFT JOIN core.legacy_pass_payment_metadata legacy ON p.legacy_source_key IS NOT NULL
 AND legacy.event_id=b.event_id AND legacy.owner=b.owner AND legacy.assigned_at=b.assigned_at
 AND legacy.received_at=p.received_at
LEFT JOIN core.pass_receiver_backfills f ON f.event_id=b.event_id AND f.owner=b.owner AND f.assigned_at=b.assigned_at
WHERE b.event_id=$1 AND b.owner=$2`, eventID, owner).Scan(&result.ReceivingAdmin)
	if err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT titles FROM core.pass_events WHERE id=$1`, eventID).
		Scan(&result.EventTitles); err != nil {
		return result, err
	}
	if err = result.readContacts(ctx, tx); err != nil {
		return result, err
	}
	if b.State == paid {
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_bookings b
 LEFT JOIN core.pass_receiver_backfills f ON f.event_id=b.event_id AND f.owner=b.owner AND f.assigned_at=b.assigned_at
 WHERE b.event_id=$1 AND (b.owner=$2 OR (b.owner=$3 AND b.partner=$2))
 AND b.state='paid' AND b.assigned_at IS NOT NULL AND b.payment_attempt IS NULL AND f.owner IS NULL)`, eventID, b.Owner, b.Partner).Scan(&result.CanBackfill)
		if err != nil {
			return result, err
		}
	}
	return result, tx.Commit(ctx)
}

func (t *TakeoverTarget) readContacts(ctx context.Context, tx pgx.Tx) error {
	for _, item := range []struct {
		owner       string
		destination **Contact
	}{{t.Booking.PaymentAdmin, &t.PaymentContact}, {t.ReceivingAdmin, &t.ReceiverContact}} {
		if item.owner == "" {
			continue
		}
		contact := &Contact{Owner: item.owner}
		if err := tx.QueryRow(ctx, `SELECT name,telegram_id FROM core.users WHERE id=$1`, item.owner).
			Scan(&contact.Name, &contact.TelegramID); err != nil {
			return err
		}
		*item.destination = contact
	}
	return nil
}
