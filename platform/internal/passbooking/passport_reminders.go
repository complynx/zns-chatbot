package passbooking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// ProcessPassportReminders preserves the Python startup-only, once-per-user
// reminder. The marker and outbox entry commit together; failure aborts startup.
func (s Service) ProcessPassportReminders(ctx context.Context) (int, error) {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('pass-passport-reminder',0))`); err != nil {
		return 0, err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT ON (b.owner) `+bookingColumns+` FROM core.pass_bookings b
 JOIN core.users u ON u.id=b.owner JOIN core.pass_profiles p ON p.owner=b.owner
 JOIN core.pass_events e ON e.id=b.event_id
 WHERE e.passport_required AND e.finishes_at>clock_timestamp() AND b.state IN ('assigned','paid')
 AND p.passport='' AND NOT EXISTS(SELECT 1 FROM core.pass_passport_reminders r WHERE r.owner=b.owner)
 ORDER BY b.owner,e.display_order,e.id`)
	if err != nil {
		return 0, err
	}
	bookings, err := pgx.CollectRows(rows, pgx.RowToStructByPos[Booking])
	if err != nil {
		return 0, err
	}
	count := 0
	for _, booking := range bookings {
		var passport string
		if err = tx.QueryRow(ctx, `SELECT passport FROM core.pass_profiles WHERE owner=$1 FOR SHARE`, booking.Owner).
			Scan(&passport); err != nil {
			return 0, err
		}
		if passport != "" {
			continue
		}
		var owner string
		err = tx.QueryRow(ctx, `INSERT INTO core.pass_passport_reminders(owner,notified_at) VALUES($1,clock_timestamp()) ON CONFLICT DO NOTHING RETURNING owner`, booking.Owner).
			Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		if err = enqueuePassNotice(ctx, tx, &booking, booking.Owner, "passport_required", "once", ""); err != nil {
			return 0, err
		}
		count++
	}
	return count, tx.Commit(ctx)
}

func (s Service) livePassportReminder(ctx context.Context, notice Notification) (Notification, error) {
	err := s.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core.pass_profiles p JOIN core.pass_bookings b ON b.owner=p.owner
 JOIN core.pass_events e ON e.id=b.event_id WHERE p.owner=$1 AND p.passport=''
 AND b.state IN ('assigned','paid') AND e.passport_required AND e.finishes_at>clock_timestamp())`, notice.Owner).
		Scan(&notice.Current)
	return notice, err
}
