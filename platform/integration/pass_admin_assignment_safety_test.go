package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassAdminAssignmentAuthorizationAndProfileCreation(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	price := 0
	command := passbooking.AdminAssignment{Event: "dance", Key: "create", Target: "alice", TotalPrice: &price,
		Create: &passbooking.AdminCreate{FromProfile: true}}
	_, err := service.AdminAssign(t.Context(), "alice", command)
	requireCode(t, err, "forbidden")
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_payment_admins(event_id,owner,hidden) VALUES('dance','visitor',true)`,
	)
	require.NoError(t, err)
	_, err = service.AdminAssign(t.Context(), "visitor", command)
	requireCode(t, err, "forbidden")
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, passallocation.Leader, alice.Role)
	assert.Equal(t, "paid", alice.State)
	name := "Synthetic Person"
	command = passbooking.AdminAssignment{Event: "dance", Key: "explicit", Target: "visitor", TotalPrice: &price,
		Create: &passbooking.AdminCreate{Role: passallocation.Follower, LegalName: &name}}
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err)
	var stored, role string
	var version int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT legal_name,role,version FROM core.pass_profiles WHERE owner='visitor'`).
			Scan(&stored, &role, &version),
	)
	assert.Equal(t, name, stored)
	assert.Empty(t, role, "explicit pass role is a snapshot; Python only updates the legal name")
	assert.EqualValues(t, 1, version)
	var field string
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT field FROM core.pass_profile_history WHERE owner='visitor'`).Scan(&field),
	)
	assert.Equal(t, "legal_name", field)
	unknown := command
	unknown.Key, unknown.Target = "unknown", "not-a-user"
	_, err = service.AdminAssign(t.Context(), "bob", unknown)
	requireCode(t, err, "forbidden")
}

func TestPassAdminAssignmentFrozenAndStaleProfile(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_profiles SET frozen=true,version=2,legal_name='Frozen Name' WHERE owner='alice'`,
	)
	require.NoError(t, err)
	price := 0
	name := "Replacement Name"
	command := passbooking.AdminAssignment{Event: "dance", Key: "frozen", Target: "alice", TotalPrice: &price,
		Create: &passbooking.AdminCreate{Role: passallocation.Leader, LegalName: &name}}
	_, err = service.AdminAssign(t.Context(), "bob", command)
	requireCode(t, err, "pass_profile_stale")
	command.Create.ProfileVersion = 2
	_, err = service.AdminAssign(t.Context(), "bob", command)
	requireCode(t, err, "pass_profile_frozen")
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Zero(t, alice.Version)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&count))
	assert.Zero(t, count)
}

func TestPassAdminAssignmentAppendGuardRollsBackProfile(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',1,20,200,now()+interval '1 day')`,
	)
	require.NoError(t, err)
	name := "Synthetic name must roll back"
	price, tier := 100, 2
	command := passbooking.AdminAssignment{
		Event:      "dance",
		Key:        "invalid-append",
		Target:     "alice",
		TotalPrice: &price,
		AppendTier: &tier,
		Create:     &passbooking.AdminCreate{Role: passallocation.Leader, LegalName: &name},
	}
	_, err = service.AdminAssign(t.Context(), "bob", command)
	requireCode(t, err, "pass_tier_invalid")
	var stored string
	var version int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT legal_name,version FROM core.pass_profiles WHERE owner='alice'`).
			Scan(&stored, &version),
	)
	assert.Empty(t, stored)
	assert.Zero(t, version)
	alice, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Zero(t, alice.Version)
	command.AppendTier = nil
	command.Version = 1
	_, err = service.AdminAssign(t.Context(), "bob", command)
	requireCode(t, err, "pass_booking_stale")
}

func TestPassAdminAssignmentAtomicFailureAndRetry(t *testing.T) {
	t.Parallel()
	db, service := adminPairFixture(t)
	_, err := db.Exec(
		t.Context(),
		`CREATE FUNCTION core.fail_free_partner() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.owner='bob' THEN RAISE EXCEPTION 'synthetic second participant failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER fail_free_partner BEFORE INSERT ON core.pass_payment_participants FOR EACH ROW EXECUTE FUNCTION core.fail_free_partner()`,
	)
	require.NoError(t, err)
	price, tier := 0, 1
	command := passbooking.AdminAssignment{
		Event:         "dance",
		Key:           "atomic",
		Version:       1,
		Target:        "alice",
		TargetVersion: 1,
		TotalPrice:    &price,
		AppendTier:    &tier,
	}
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.Error(t, err)
	for _, owner := range []string{"alice", "bob"} {
		booking, readErr := service.Get(t.Context(), owner, "dance")
		require.NoError(t, readErr)
		assert.Equal(t, "waitlist", booking.State)
		assert.EqualValues(t, 1, booking.Version)
		assert.NotEmpty(t, booking.Partner)
	}
	var count, amount int
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_payment_attempts`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&count))
	assert.Zero(t, count)
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT amount FROM core.pass_event_tiers WHERE event_id='dance' AND position=0`).
			Scan(&amount),
	)
	assert.Equal(t, 20, amount)
	_, err = db.Exec(t.Context(), `DROP TRIGGER fail_free_partner ON core.pass_payment_participants`)
	require.NoError(t, err)
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err, "rolled-back operation key remains retryable")
}

func TestPassAdminAssignmentConcurrentStaleAndTriState(t *testing.T) {
	t.Parallel()
	_, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	price := 0
	skip := true
	command := passbooking.AdminAssignment{
		Event:         "dance",
		Key:           "race-a",
		Target:        "alice",
		TargetVersion: alice.Version,
		TotalPrice:    &price,
		SkipBalance:   &skip,
	}
	var group sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{"race-a", "race-b"} {
		group.Go(func() {
			request := command
			request.Key = key
			_, executeErr := service.AdminAssign(t.Context(), "bob", request)
			results <- executeErr
		})
	}
	group.Wait()
	close(results)
	successes := 0
	for executeErr := range results {
		if executeErr == nil {
			successes++
		} else {
			requireCode(t, executeErr, "pass_booking_stale")
		}
	}
	assert.Equal(t, 1, successes)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	command = passbooking.AdminAssignment{
		Event:         "dance",
		Key:           "preserve-skip",
		Target:        "alice",
		TargetVersion: alice.Version,
	}
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.NotNil(t, alice.SkipBalance)
	assert.True(t, *alice.SkipBalance)
	skip = false
	command.Key, command.TargetVersion, command.SkipBalance = "include", alice.Version, &skip
	_, err = service.AdminAssign(t.Context(), "bob", command)
	require.NoError(t, err)
	alice, err = service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.False(t, *alice.SkipBalance)
}

func TestPassAdminAssignmentFreshClockAfterEventLock(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	lock, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Rollback(context.WithoutCancel(t.Context())) })
	_, err = lock.Exec(t.Context(), `SELECT id FROM core.pass_events WHERE id='dance' FOR UPDATE`)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() {
		_, executeErr := service.AdminAssign(
			t.Context(),
			"bob",
			passbooking.AdminAssignment{Event: "dance", Key: "deadline", Target: "alice", TargetVersion: alice.Version},
		)
		result <- executeErr
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM core.pass_events%FOR UPDATE%')`).
			Scan(&waiting)
		return queryErr == nil && waiting
	}, time.Second, 10*time.Millisecond)
	_, err = lock.Exec(t.Context(), `UPDATE core.pass_events SET finishes_at=clock_timestamp() WHERE id='dance'`)
	require.NoError(t, err)
	require.NoError(t, lock.Commit(t.Context()))
	select {
	case err = <-result:
		requireCode(t, err, "pass_event_finished")
	case <-time.After(5 * time.Second):
		t.Fatal("admin assignment remained blocked")
	}
}
