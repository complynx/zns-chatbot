package passbooking

import (
	"context"
	"errors"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking/dbgen"
)

// ProcessPassportReminders preserves the Python startup-only, once-per-user
// reminder. The marker and outbox entry commit together; failure aborts startup.
func (s Service) ProcessPassportReminders(ctx context.Context) (int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	s, clockAttempt := s.WithClockAttempt()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pass-passport-reminder',0))`); err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (b.owner) `+bookingColumns+` FROM core.pass_bookings b
 JOIN core.users u ON u.id=b.owner JOIN core.pass_profiles p ON p.owner=b.owner
 JOIN core.pass_events e ON e.id=b.event_id
 WHERE e.passport_required AND e.finishes_at>COALESCE($1::timestamptz,clock_timestamp()) AND b.state IN ('assigned','paid')
 AND p.passport='' AND NOT EXISTS(SELECT 1 FROM core.pass_passport_reminders r WHERE r.owner=b.owner)
 ORDER BY b.owner,e.display_order,e.id`, observed)
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	bookings, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Booking])
	if err != nil {
		return 0, core.DatabaseOperationError(err)
	}
	var pending []delivery.Registration
	count := 0
	for _, booking := range bookings {
		var passport string
		if err = tx.QueryRow(ctx, `SELECT passport FROM core.pass_profiles WHERE owner=$1 FOR SHARE`, booking.Owner).
			Scan(&passport); err != nil {
			return 0, core.DatabaseOperationError(err)
		}
		eligible, eligibilityErr := s.passportReminderEligible(ctx, tx, booking, passport)
		if eligibilityErr != nil {
			return 0, eligibilityErr
		}
		// Before the first marker, profile waits can renew the selection safely.
		// Later advances must roll back every earlier marker in this transaction.
		clockAttempt.acceptCurrent(count == 0)
		if !eligible {
			continue
		}
		var owner string
		err = tx.QueryRow(ctx, `INSERT INTO core.pass_passport_reminders(owner,notified_at) VALUES($1,clock_timestamp()) ON CONFLICT DO NOTHING RETURNING owner`, booking.Owner).
			Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, core.DatabaseOperationError(err)
		}
		if err = enqueuePassNotice(ctx, tx, s.Delivery.BotID, &pending,
			&booking,
			booking.Owner,
			"passport_required",
			"once",
			"",
		); err != nil {
			return 0, err
		}
		count++
	}
	if err = delivery.RegisterBatch(ctx, tx, s.Delivery.BotID, pending); err != nil {
		return 0, err
	}
	if err = clockAttempt.Check(ctx); err != nil {
		return 0, err
	}
	return count, core.DatabaseOperationError(tx.Commit(ctx))
}

func (s Service) passportReminderEligible(
	ctx context.Context,
	tx pgx.Tx,
	booking Booking,
	passport string,
) (bool, error) {
	if passport != "" {
		return false, nil
	}
	if s.RegistrationClock == nil {
		return true, nil
	}
	fresh, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return false, err
	}
	var current bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_bookings b
 JOIN core.pass_events e ON e.id=b.event_id WHERE b.owner=$1 AND b.event_id=$2
 AND b.created_at=$3 AND b.state IN ('assigned','paid') AND e.passport_required AND e.finishes_at>$4)`,
		booking.Owner, booking.Event, booking.CreatedAt, fresh).Scan(&current)
	return current, core.DatabaseOperationError(err)
}

func (s Service) livePassportReminder(
	ctx context.Context,
	reader dbgen.DBTX,
	notice Notification,
) (Notification, error) {
	observed, err := registrationSQLTime(ctx, s.RegistrationClock)
	if err != nil {
		return notice, err
	}
	err = reader.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_profiles p JOIN core.pass_bookings b ON b.owner=p.owner
 JOIN core.pass_events e ON e.id=b.event_id WHERE p.owner=$1 AND p.passport=''
 AND b.state IN ('assigned','paid') AND e.passport_required AND e.finishes_at>COALESCE($2::timestamptz,clock_timestamp()))`, notice.Owner, observed).
		Scan(&notice.Current)
	return notice, core.DatabaseOperationError(err)
}
