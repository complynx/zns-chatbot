package integration_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestPassMaintenancePreservesLaterSQLFailure(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `
INSERT INTO core.pass_events(id,finishes_at)
 VALUES('bad-domain',clock_timestamp()+interval '30 days'),('bad-sql',clock_timestamp()+interval '30 days');
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('bad-domain',1,20,100,clock_timestamp()-interval '1 day'),
 ('bad-sql',0,20,100,clock_timestamp()-interval '1 day');
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('bad-domain','alice',1,'waitlist','leader','solo','bob',clock_timestamp()),
 ('bad-sql','alice',1,'waitlist','leader','solo','bob',clock_timestamp()),
 ('dance','alice',1,'waitlist','leader','solo','bob',clock_timestamp());
CREATE SEQUENCE core.maintenance_fault_attempt;
CREATE FUNCTION core.reject_test_maintenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.event_id='bad-sql' THEN
  PERFORM nextval('core.maintenance_fault_attempt');
  RAISE EXCEPTION 'private maintenance SQL diagnostic';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_test_maintenance BEFORE INSERT OR UPDATE ON core.pass_bookings
FOR EACH ROW EXECUTE FUNCTION core.reject_test_maintenance()`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	requireCode(t, err, "pass_tiers_invalid")
	var called bool
	require.NoError(t, db.QueryRow(t.Context(), `SELECT is_called FROM core.maintenance_fault_attempt`).Scan(&called))
	require.True(t, called)
	healthy, readErr := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, readErr)
	require.Equal(t, "assigned", healthy.State, "later healthy events must still commit")
	failed, readErr := service.Get(t.Context(), "alice", "bad-sql")
	require.NoError(t, readErr)
	require.Equal(t, "waitlist", failed.State)
	require.EqualValues(t, 1, failed.Version)
	require.ErrorIs(t, err, core.ErrDatabase, "a preceding domain failure must not hide SQL provenance")
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "private maintenance")
	_, err = db.Exec(t.Context(), `DROP TRIGGER reject_test_maintenance ON core.pass_bookings;
UPDATE core.pass_event_tiers SET position=0 WHERE event_id='bad-domain'`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	for _, event := range []string{"bad-domain", "bad-sql", "dance"} {
		current, getErr := service.Get(t.Context(), "alice", event)
		require.NoError(t, getErr)
		require.Equal(t, "assigned", current.State)
		var notices int
		require.NoError(t, db.QueryRow(
			t.Context(),
			`SELECT count(*) FROM core.pass_notifications WHERE event_id=$1 AND owner='alice' AND kind='assigned'`,
			event,
		).
			Scan(&notices))
		require.Equal(t, 1, notices)
	}
}

func TestPassMaintenanceKeepsSQLFailureAtCancellation(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.pass_bookings
(event_id,owner,version,state,role,kind,payment_admin,created_at)
VALUES('dance','alice',1,'waitlist','leader','solo','bob',clock_timestamp())`)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	service.Intake = maintenanceFailureIntake(func(queryCtx context.Context, _ string) error {
		_, queryErr := db.Exec(queryCtx, `SELECT 1/0`)
		require.Error(t, queryErr)
		cancel()
		return core.DatabaseOperationError(queryErr)
	})
	_, err = service.ProcessDeadlines(ctx)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.True(t, core.IsDatabaseFailure(err))
	require.NotContains(t, err.Error(), "division by zero")
}

type maintenanceFailureIntake func(context.Context, string) error

func (resolve maintenanceFailureIntake) ResolveRegistrationIntake(ctx context.Context, event string) error {
	return resolve(ctx, event)
}
