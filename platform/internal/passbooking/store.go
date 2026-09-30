package passbooking

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
)

const bookingColumns = `b.event_id,b.owner,u.telegram_id,b.version,b.state,b.role,b.kind,b.partner,
b.invitation_target,b.payment_admin,b.created_at,b.assigned_at,b.price,b.tier_index,b.skip_balance,b.comment,b.invitation_started_at`

func readEvent(ctx context.Context, tx pgx.Tx, id string) (event, error) {
	return readEventLocked(ctx, tx, id, true)
}

func readEventLocked(ctx context.Context, tx pgx.Tx, id string, allocation bool) (event, error) {
	e := event{id: id, admins: map[string]bool{}}
	query := `SELECT finishes_at,passport_required,assignment_rule,disable_concurrency_limit FROM core.pass_events WHERE id=$1`
	if allocation {
		query += ` FOR NO KEY UPDATE`
	}
	err := tx.QueryRow(ctx, query, id).
		Scan(&e.finishes, &e.passport, &e.rule, &e.unlimited)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, conflict("pass_event_unknown")
	}
	if err != nil {
		return e, core.DatabaseOperationContextError(ctx, err)
	}
	rows, err := tx.Query(
		ctx,
		`SELECT amount,price,starts_at,promo,blocked_by_date FROM core.pass_event_tiers WHERE event_id=$1 ORDER BY position`,
		id,
	)
	if err != nil {
		return e, core.DatabaseOperationContextError(ctx, err)
	}
	e.tiers, err = pgx.CollectRows(rows, pgx.RowToStructByPos[passallocation.Tier])
	if err != nil {
		return e, core.DatabaseOperationContextError(ctx, err)
	}
	var invalidPositions bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM (SELECT position,row_number() OVER(ORDER BY position)-1 expected FROM core.pass_event_tiers WHERE event_id=$1) t WHERE position<>expected)`, id).
		Scan(&invalidPositions)
	if err != nil {
		return e, core.DatabaseOperationContextError(ctx, err)
	}
	if invalidPositions {
		return e, conflict("pass_tiers_invalid")
	}
	rows, err = tx.Query(ctx, `SELECT owner,hidden FROM core.pass_payment_admins WHERE event_id=$1 ORDER BY owner`, id)
	if err != nil {
		return e, core.DatabaseOperationContextError(ctx, err)
	}
	defer rows.Close()
	for rows.Next() {
		var owner string
		var hidden bool
		if err = rows.Scan(&owner, &hidden); err != nil {
			return e, core.DatabaseOperationContextError(ctx, err)
		}
		e.admins[owner] = hidden
	}
	return e, core.DatabaseOperationContextError(ctx, rows.Err())
}

func readBookings(ctx context.Context, tx pgx.Tx, eventID string) (map[string]*Booking, error) {
	rows, err := tx.Query(
		ctx,
		`SELECT `+bookingColumns+` FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner WHERE b.event_id=$1`,
		eventID,
	)
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	records, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Booking])
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	result := make(map[string]*Booking, len(records))
	for _, b := range records {
		result[b.Owner] = &b
	}
	return result, nil
}

func persist(ctx context.Context, tx pgx.Tx, s *snapshot) error {
	if err := s.persistAdmissions(ctx, tx); err != nil {
		return err
	}
	for owner := range s.dirty {
		b := s.bookings[owner]
		_, err := tx.Exec(
			ctx,
			`INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,partner,invitation_target,payment_admin,created_at,assigned_at,price,tier_index,skip_balance,comment,invitation_started_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
ON CONFLICT(event_id,owner) DO UPDATE SET version=EXCLUDED.version,state=EXCLUDED.state,role=EXCLUDED.role,kind=EXCLUDED.kind,partner=EXCLUDED.partner,invitation_target=EXCLUDED.invitation_target,payment_admin=EXCLUDED.payment_admin,created_at=EXCLUDED.created_at,assigned_at=EXCLUDED.assigned_at,price=EXCLUDED.price,tier_index=EXCLUDED.tier_index,skip_balance=EXCLUDED.skip_balance,comment=EXCLUDED.comment,invitation_started_at=EXCLUDED.invitation_started_at`,
			b.Event,
			b.Owner,
			b.Version,
			b.State,
			b.Role,
			b.Kind,
			b.Partner,
			b.InvitationTarget,
			b.PaymentAdmin,
			b.CreatedAt,
			b.AssignedAt,
			b.Price,
			b.TierIndex,
			b.SkipBalance,
			b.Comment,
			b.InvitationStartedAt,
		)
		if err != nil {
			return fmt.Errorf("persist pass booking: %w", core.DatabaseOperationError(err))
		}
		if b.State == cancelled || b.State == waitlist || b.State == pending {
			if _, err = tx.Exec(
				ctx,
				`UPDATE core.pass_bookings SET payment_attempt=NULL WHERE event_id=$1 AND owner=$2`,
				b.Event,
				b.Owner,
			); err != nil {
				return core.DatabaseOperationError(err)
			}
		}
	}
	return nil
}

func (s Service) Get(ctx context.Context, actor, eventID string) (Booking, error) {
	var exists bool
	if err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.users WHERE id=$1)`, actor).
		Scan(&exists); err != nil {
		return Booking{}, core.DatabaseOperationError(err)
	}
	if !exists {
		return Booking{}, forbidden()
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT `+bookingColumns+` FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner WHERE b.event_id=$1 AND b.owner=$2`,
		eventID,
		actor,
	)
	if err != nil {
		return Booking{}, core.DatabaseOperationError(err)
	}
	b, err := pgx.CollectOneRow(rows, pgx.RowToStructByPos[Booking])
	if errors.Is(err, pgx.ErrNoRows) {
		return Booking{Event: eventID, Owner: actor}, nil
	}
	return b, core.DatabaseOperationError(err)
}

// Queue is restricted to global booking administrators; owners use Get.
func (s Service) Queue(ctx context.Context, actor, eventID, after string) (BookingPage, error) {
	at, telegramID, err := parsePageCursor(after)
	if err != nil {
		return BookingPage{}, err
	}
	rows, err := s.DB.Query(
		ctx,
		`SELECT `+bookingColumns+`,u.name FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
WHERE b.event_id=$1 AND EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$2)
AND ($3='' OR (b.created_at,u.telegram_id)>($4,$5))
ORDER BY b.created_at,u.telegram_id LIMIT $6`,
		eventID,
		actor,
		after,
		at,
		telegramID,
		pageSize+1,
	)
	if err != nil {
		return BookingPage{}, core.DatabaseOperationError(err)
	}
	type namedBooking struct {
		Booking

		Name string
	}
	result, err := pgx.CollectRows(rows, pgx.RowToStructByPos[namedBooking])
	if err != nil {
		return BookingPage{}, core.DatabaseOperationError(err)
	}
	var allowed bool
	if err = s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_booking_admins WHERE owner=$1)`, actor).
		Scan(&allowed); err != nil {
		return BookingPage{}, core.DatabaseOperationError(err)
	}
	if !allowed {
		return BookingPage{}, forbidden()
	}
	page := BookingPage{Bookings: []Booking{}, Names: map[string]string{}}
	if len(result) > pageSize {
		last := result[pageSize-1]
		page.Next = pageCursor(last.CreatedAt, last.TelegramID)
		result = result[:pageSize]
	}
	for _, item := range result {
		page.Bookings = append(page.Bookings, item.Booking)
		page.Names[item.Owner] = item.Name
	}
	return page, nil
}

func newSnapshot(e event, bookings map[string]*Booking, now time.Time) *snapshot {
	return &snapshot{event: e, bookings: bookings, dirty: map[string]bool{}, now: now}
}
