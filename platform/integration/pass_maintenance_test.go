package integration_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassMaintenanceOpensDatedTierWithoutUserAction(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0;
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at,blocked_by_date)
VALUES('dance',1,20,200,clock_timestamp()+interval '1 day',true)`)
	require.NoError(t, err)
	waiting, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	require.Equal(t, "waitlist", waiting.State)
	drainPassDomainNotices(t, service)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	unchanged, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, waiting.Version, unchanged.Version)
	require.Empty(t, drainPassDomainNotices(t, service))
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_event_tiers SET starts_at=clock_timestamp()-interval '1 second' WHERE position=1`,
	)
	require.NoError(t, err)
	// Two scanners model a repeated startup/tick. The event lock must make
	// assignment and its notification unique without a user command.
	var group sync.WaitGroup
	results := make(chan error, 2)
	for range 2 {
		group.Go(func() {
			_, scanErr := (passbooking.Service{DB: db, Delivery: service.Delivery}).ProcessDeadlines(t.Context())
			results <- scanErr
		})
	}
	group.Wait()
	close(results)
	for scanErr := range results {
		require.NoError(t, scanErr)
	}
	assigned, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", assigned.State)
	assert.Greater(t, assigned.Version, waiting.Version)
	require.NotNil(t, assigned.Price)
	assert.Equal(t, 200, *assigned.Price)
	assert.Equal(t, []string{"assigned"}, noticeKinds(drainPassDomainNotices(t, service), "alice"))
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	require.Empty(t, drainPassDomainNotices(t, service))
}

func TestPassMaintenancePagesPastBlockedQueuesAndSkipsClosedEvents(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `
INSERT INTO core.pass_events(id,finishes_at)
 SELECT 'blocked-'||lpad(n::text,3,'0'),clock_timestamp()+interval '30 days' FROM generate_series(1,101) n;
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at,blocked_by_date)
 SELECT id,0,20,100,clock_timestamp()+interval '1 day',true FROM core.pass_events WHERE id LIKE 'blocked-%';
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 SELECT id,'alice',1,'waitlist','leader','solo','bob',clock_timestamp() FROM core.pass_events WHERE id LIKE 'blocked-%';
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('dance','alice',1,'waitlist','leader','solo','bob',clock_timestamp());
INSERT INTO core.pass_events(id,finishes_at) VALUES('closed',clock_timestamp()-interval '1 second');
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('closed',0,20,100,clock_timestamp()-interval '1 day');
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('closed','alice',1,'waitlist','leader','solo','bob',clock_timestamp());`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	assigned, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", assigned.State, "blocked first page must not starve an open later event")
	closed, err := service.Get(t.Context(), "alice", "closed")
	require.NoError(t, err)
	assert.Equal(t, "waitlist", closed.State)
	assert.EqualValues(t, 1, closed.Version)
	var blocked int
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_bookings WHERE event_id LIKE 'blocked-%' AND state='waitlist' AND version=1`).
			Scan(&blocked),
	)
	assert.Equal(t, 101, blocked)
}

func TestPassMaintenanceIsolatesInvalidEvent(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(t.Context(), `
INSERT INTO core.pass_events(id,finishes_at) VALUES('bad',clock_timestamp()+interval '30 days');
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('bad',1,20,100,clock_timestamp()-interval '1 day');
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at)
 VALUES('bad','alice',1,'waitlist','leader','solo','bob',clock_timestamp()),
 ('dance','alice',1,'waitlist','leader','solo','bob',clock_timestamp());`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	requireCode(t, err, "pass_tiers_invalid")
	assigned, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", assigned.State)
	bad, err := service.Get(t.Context(), "alice", "bad")
	require.NoError(t, err)
	assert.Equal(t, "waitlist", bad.State)
	assert.EqualValues(t, 1, bad.Version)
	assert.Equal(t, []string{"assigned"}, noticeKinds(drainPassDomainNotices(t, service), "alice"))
	// Repair permits a later scan to recover the failed event normally.
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET position=0 WHERE event_id='bad'`)
	require.NoError(t, err)
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	bad, err = service.Get(t.Context(), "alice", "bad")
	require.NoError(t, err)
	assert.Equal(t, "assigned", bad.State)
}
