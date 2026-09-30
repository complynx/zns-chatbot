package integration_test

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassNotificationSQLFailureRollsBackRegistration(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `CREATE SEQUENCE core.notice_fault_attempt;
CREATE FUNCTION core.reject_test_notice() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM nextval('core.notice_fault_attempt');
 RAISE EXCEPTION 'private notification SQL diagnostic';
END $$;
CREATE TRIGGER reject_test_notice BEFORE INSERT ON core.pass_notifications
FOR EACH ROW EXECUTE FUNCTION core.reject_test_notice()`)
	require.NoError(t, err)
	command := bookingCommand("solo", "notice-sql-recovery", passbooking.Booking{})
	_, err = service.Execute(t.Context(), "alice", command)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "private notification")
	var driverError *pgconn.PgError
	require.NotErrorAs(t, err, &driverError)
	var attempts int64
	var called bool
	require.NoError(t, db.QueryRow(t.Context(),
		`SELECT last_value,is_called FROM core.notice_fault_attempt`).Scan(&attempts, &called))
	require.True(t, called, "the failure must originate at the notification insert")
	require.EqualValues(t, 1, attempts)
	var bookings, notices, receipts int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.pass_bookings WHERE owner='alice'),
 (SELECT count(*) FROM core.pass_notifications WHERE owner='alice'),
 (SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice')`).Scan(&bookings, &notices, &receipts))
	require.Zero(t, bookings)
	require.Zero(t, notices)
	require.Zero(t, receipts)
	_, err = db.Exec(t.Context(), `DROP TRIGGER reject_test_notice ON core.pass_notifications`)
	require.NoError(t, err)
	current, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Positive(t, current.Version)
	replayed, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Equal(t, current, replayed)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.pass_notifications WHERE owner='alice' AND kind='registered'),
 (SELECT count(*) FROM core.pass_booking_operations WHERE actor='alice')`).Scan(&notices, &receipts))
	require.Equal(t, 1, notices)
	require.Equal(t, 1, receipts)
}
