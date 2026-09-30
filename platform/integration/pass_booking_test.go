package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passallocation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func TestPassBookingRechecksFinishAfterLock(t *testing.T) {
	t.Parallel()
	for _, resource := range []string{"event", "profile"} {
		t.Run(resource, func(t *testing.T) {
			t.Parallel()
			db, s := bookingFixture(t)
			var deadline time.Time
			require.NoError(
				t,
				db.QueryRow(t.Context(), `UPDATE core.pass_events SET finishes_at=clock_timestamp()+interval '2 seconds' WHERE id='dance' RETURNING finishes_at`).
					Scan(&deadline),
			)
			waitStarted := time.Now()
			lock, err := db.Begin(t.Context())
			require.NoError(t, err)
			t.Cleanup(func() { _ = lock.Rollback(context.WithoutCancel(t.Context())) })
			query := `SELECT id FROM core.pass_events WHERE id='dance' FOR NO KEY UPDATE`
			waitingQuery := `%FROM core.pass_events%FOR NO KEY UPDATE%`
			if resource == "profile" {
				query = `SELECT owner FROM core.pass_profiles WHERE owner='alice' FOR UPDATE`
				waitingQuery = `%FROM core.pass_profiles%FOR SHARE%`
			}
			_, err = lock.Exec(t.Context(), query)
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() {
				_, executeErr := s.Execute(
					t.Context(),
					"alice",
					bookingCommand("solo", "deadline", passbooking.Booking{}),
				)
				result <- executeErr
			}()
			require.Eventually(t, func() bool {
				var waiting bool
				queryErr := db.QueryRow(t.Context(), `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, waitingQuery).
					Scan(&waiting)
				return queryErr == nil && waiting
			}, time.Second, 10*time.Millisecond, "booking must reach the held lock before the deadline")
			if resource == "event" {
				// Close sales while the transaction is known to be waiting. The
				// locked reader must observe the committed deadline and a fresh clock.
				_, err = lock.Exec(
					t.Context(),
					`UPDATE core.pass_events SET finishes_at=clock_timestamp() WHERE id='dance'`,
				)
				require.NoError(t, err)
			} else {
				// Docker's wall clock can briefly jump forward and return. Also
				// require the original two-second interval on a monotonic clock,
				// so one transient SQL sample cannot release the profile lock early.
				require.Eventually(t, func() bool {
					if time.Since(waitStarted) < 2*time.Second {
						return false
					}
					var expired bool
					queryErr := db.QueryRow(t.Context(), `SELECT clock_timestamp()>$1`, deadline).Scan(&expired)
					return queryErr == nil && expired
				}, 10*time.Second, 10*time.Millisecond)
			}
			require.NoError(t, lock.Commit(t.Context()))
			select {
			case err = <-result:
				requireCode(t, err, "pass_sales_closed")
			case <-time.After(5 * time.Second):
				t.Fatal("booking remained blocked")
			}
			booking, err := s.Get(t.Context(), "alice", "dance")
			require.NoError(t, err)
			assert.Zero(t, booking.Version)
			var operations int
			require.NoError(
				t,
				db.QueryRow(t.Context(), `SELECT count(*) FROM core.pass_booking_operations`).Scan(&operations),
			)
			assert.Zero(t, operations)
		})
	}
}

func bookingFixture(t *testing.T) (*pgxpool.Pool, passbooking.Service) {
	t.Helper()
	db := database(t)
	_, err := db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at) VALUES('dance',now()+interval '30 days');
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',0,20,100,now()-interval '1 day');
INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance','bob');
INSERT INTO core.pass_booking_admins(owner) VALUES('bob');
INSERT INTO core.pass_profiles(owner,role) VALUES('alice','leader'),('bob','follower');`,
	)
	require.NoError(t, err)
	return db, passbooking.Service{DB: db, Delivery: syntheticDeliverySettings()}
}

func bookingCommand(name, key string, b passbooking.Booking) passbooking.Command {
	return passbooking.Command{Name: name, Event: "dance", Key: key, Version: b.Version}
}

func TestPassBookingCoupleAtomicAndRestart(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	assert.Equal(t, "waiting-for-couple", alice.State)
	accept := bookingCommand("accept", "accept", passbooking.Booking{})
	accept.Target = "alice"
	accept.TargetVersion = alice.Version
	_, err = s.Execute(t.Context(), "visitor", accept)
	requireCode(t, err, "forbidden")
	bob, err := s.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	assert.Equal(t, "assigned", bob.State)
	assert.Equal(t, passallocation.Follower, bob.Role)
	assert.Equal(t, "alice", bob.Partner)
	require.NotNil(t, bob.Price)
	assert.Equal(t, 100, *bob.Price)
	s = passbooking.Service{DB: db}
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", alice.State)
	assert.Equal(t, "bob", alice.Partner)
	assert.Equal(t, alice.CreatedAt, bob.CreatedAt)
	replay, err := s.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	assert.Equal(t, bob, replay)
	accept.TargetVersion++
	_, err = s.Execute(t.Context(), "bob", accept)
	requireCode(t, err, "idempotency_conflict")
	profile, err := (passes.Service{DB: db}).Execute(
		t.Context(),
		"alice",
		passes.Command{Name: "set", Field: "role", Value: "follower", Key: "role-change", Origin: "manual"},
	)
	require.NoError(t, err)
	assert.Equal(t, "follower", profile.Role)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, passallocation.Leader, alice.Role)
	alice, err = s.Execute(t.Context(), "alice", bookingCommand("cancel", "cancel", alice))
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
	bob, err = s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", bob.State)
	_, err = s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          "accept",
			Event:         "dance",
			Key:           "stale",
			Version:       bob.Version,
			Target:        "alice",
			TargetVersion: 1,
		},
	)
	requireCode(t, err, "pass_invitation_stale")
}

func TestPassBookingGuardsAndUnknownInvitee(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "identity", passbooking.Booking{}))
	requireCode(t, err, "pass_identity_required")
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET passport_required=false; UPDATE core.pass_event_tiers SET starts_at=now()+interval '1 day'`,
	)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "future", passbooking.Booking{}))
	requireCode(t, err, "pass_sales_closed")
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET starts_at=now()-interval '1 day',amount=0`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "unknown", passbooking.Booking{})
	invite.InviteTelegramID = 999
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          "accept",
			Event:         "dance",
			Key:           "forged",
			Target:        "alice",
			TargetVersion: alice.Version,
		},
	)
	requireCode(t, err, "pass_invitation_stale")
	_, err = db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book) VALUES('new',999,'New',true)`)
	require.NoError(t, err)
	newcomer, err := s.Execute(
		t.Context(),
		"new",
		passbooking.Command{Name: "accept", Event: "dance", Key: "join", Target: "alice", TargetVersion: alice.Version},
	)
	require.NoError(t, err)
	assert.Equal(t, "waitlist", newcomer.State)
	assert.Equal(t, passallocation.Follower, newcomer.Role)
	_, err = s.Queue(t.Context(), "alice", "dance", "")
	requireCode(t, err, "forbidden")
	_, err = s.Execute(
		t.Context(),
		"alice",
		passbooking.Command{
			Name:          "admin_cancel",
			Event:         "dance",
			Key:           "forged-admin",
			Version:       alice.Version,
			Target:        "new",
			TargetVersion: newcomer.Version,
		},
	)
	requireCode(t, err, "forbidden")
	_, err = db.Exec(t.Context(), `UPDATE core.pass_payment_admins SET hidden=true`)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"new",
		passbooking.Command{
			Name:         "payment_admin",
			Event:        "dance",
			Key:          "hidden",
			Version:      newcomer.Version,
			PaymentAdmin: "bob",
		},
	)
	requireCode(t, err, "pass_booking_invalid")
}

func TestPassBookingConcurrentAcceptCancel(t *testing.T) {
	t.Parallel()
	_, s := bookingFixture(t)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	var wg sync.WaitGroup
	results := make(chan error, 2)
	start := make(chan struct{})
	wg.Go(func() {
		<-start
		_, e := s.Execute(t.Context(), "alice", bookingCommand("cancel", "cancel", alice))
		results <- e
	})
	wg.Go(func() {
		<-start
		_, e := s.Execute(
			t.Context(),
			"bob",
			passbooking.Command{
				Name:          "accept",
				Event:         "dance",
				Key:           "accept",
				Target:        "alice",
				TargetVersion: alice.Version,
			},
		)
		results <- e
	})
	close(start)
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		} else {
			require.Error(t, e)
		}
	}
	assert.Equal(t, 1, successes)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err := s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	if alice.State == "assigned" {
		assert.Equal(t, "assigned", bob.State)
		assert.Equal(t, "bob", alice.Partner)
		assert.Equal(t, "alice", bob.Partner)
	} else {
		assert.Equal(t, "cancelled", alice.State)
		assert.Zero(t, bob.Version)
	}
}

func TestPassBookingQueueDoubleSoloPartialCapacity(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	alice, err := s.Execute(t.Context(), "alice", bookingCommand("solo", "alice", passbooking.Booking{}))
	require.NoError(t, err)
	bob, err := s.Execute(t.Context(), "bob", bookingCommand("solo", "bob", passbooking.Booking{}))
	require.NoError(t, err)
	assert.Equal(t, "waitlist", alice.State)
	assert.Equal(t, "waitlist", bob.State)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=1`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", bookingCommand("recalculate", "recalc", bob))
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err = s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", alice.State)
	assert.Equal(t, "waitlist", bob.State)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=2`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", bookingCommand("recalculate", "recalc2", bob))
	require.NoError(t, err)
	bob, err = s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", bob.State)
}

func TestPassBookingAdminUncoupleAndPaidCancellation(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	bob, err := s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          "accept",
			Event:         "dance",
			Key:           "accept",
			Target:        "alice",
			TargetVersion: alice.Version,
		},
	)
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_bookings SET state='paid' WHERE event_id='dance'`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "alice", bookingCommand("cancel", "deny", alice))
	requireCode(t, err, "pass_booking_state")
	_, err = s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          "admin_uncouple",
			Event:         "dance",
			Key:           "uncouple",
			Version:       bob.Version,
			Target:        "alice",
			TargetVersion: alice.Version,
		},
	)
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	bob, err = s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Equal(t, "solo", alice.Kind)
	assert.Empty(t, bob.Partner)
	assert.Equal(t, 100, *alice.Price)
	assert.Equal(t, 100, *bob.Price)
	_, err = s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          "admin_cancel",
			Event:         "dance",
			Key:           "cancel",
			Version:       bob.Version,
			Target:        "alice",
			TargetVersion: alice.Version,
		},
	)
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "cancelled", alice.State)
}

func TestPassBookingPairRollbackAndSplitTier(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET assignment_rule='paired';
UPDATE core.pass_event_tiers SET amount=2;
INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at) VALUES('dance',1,2,200,now()+interval '1 day');
INSERT INTO core.users(id,telegram_id,name,can_book) VALUES('old',404,'Old',true);
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index,skip_balance)
VALUES('dance','old',1,'paid','leader','solo','bob',now()-interval '2 days',now()-interval '1 day',100,0,true);
CREATE FUNCTION core.fail_pair() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.owner='bob' THEN RAISE EXCEPTION 'injected write failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER fail_pair BEFORE INSERT ON core.pass_bookings FOR EACH ROW EXECUTE FUNCTION core.fail_pair();`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	accept := passbooking.Command{
		Name:          "accept",
		Event:         "dance",
		Key:           "accept",
		Target:        "alice",
		TargetVersion: alice.Version,
	}
	_, err = s.Execute(t.Context(), "bob", accept)
	require.ErrorIs(t, err, core.ErrDatabase)
	require.NotContains(t, err.Error(), "injected write failure")
	unchanged, err := s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, alice, unchanged)
	bob, err := s.Get(t.Context(), "bob", "dance")
	require.NoError(t, err)
	assert.Zero(t, bob.Version)
	_, err = db.Exec(t.Context(), `DROP TRIGGER fail_pair ON core.pass_bookings`)
	require.NoError(t, err)
	bob, err = s.Execute(t.Context(), "bob", accept)
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", alice.State)
	require.NotNil(t, alice.Price)
	require.NotNil(t, bob.Price)
	assert.Equal(t, 200, *alice.Price)
	assert.Equal(t, 100, *bob.Price)
}

func TestPassBookingCapacityCountsExcludedParticipants(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `INSERT INTO core.users(id,telegram_id,name,can_book)
SELECT 'held'||n,1000+n,'Held',true FROM generate_series(1,10) n;
INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price,tier_index,skip_balance)
SELECT 'dance','held'||n,1,'assigned',CASE WHEN n%2=0 THEN 'leader' ELSE 'follower' END,'solo','bob',now()-interval '2 days',now()-interval '1 day',100,0,true FROM generate_series(1,10) n;`)
	require.NoError(t, err)
	alice, err := s.Execute(t.Context(), "alice", bookingCommand("solo", "solo", passbooking.Booking{}))
	require.NoError(t, err)
	assert.Equal(t, "waitlist", alice.State)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET disable_concurrency_limit=true`)
	require.NoError(t, err)
	_, err = s.Execute(t.Context(), "bob", bookingCommand("recalculate", "unlimited", passbooking.Booking{}))
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "assigned", alice.State)
}

func TestPassBookingSwitchPreservesSignupAndUnknownDecline(t *testing.T) {
	t.Parallel()
	db, s := bookingFixture(t)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET amount=0`)
	require.NoError(t, err)
	invite := bookingCommand("invite", "invite", passbooking.Booking{})
	invite.InviteTelegramID = 202
	alice, err := s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	before := alice
	_, err = db.Exec(t.Context(), `UPDATE core.pass_profiles SET role='follower' WHERE owner='alice'`)
	require.NoError(t, err)
	alice, err = s.Execute(t.Context(), "alice", bookingCommand("solo", "solo", alice))
	require.NoError(t, err)
	assert.Equal(t, before.Role, alice.Role)
	assert.Equal(t, before.CreatedAt, alice.CreatedAt)
	invite = bookingCommand("invite", "again", alice)
	invite.InviteTelegramID = 202
	alice, err = s.Execute(t.Context(), "alice", invite)
	require.NoError(t, err)
	_, err = s.Execute(
		t.Context(),
		"bob",
		passbooking.Command{
			Name:          "decline",
			Event:         "dance",
			Key:           "decline",
			Target:        "alice",
			TargetVersion: alice.Version,
		},
	)
	require.NoError(t, err)
	alice, err = s.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	assert.Equal(t, "waitlist", alice.State)
	assert.Equal(t, "solo", alice.Kind)
	assert.Zero(t, alice.InvitationTarget)
	assert.Equal(t, before.CreatedAt, alice.CreatedAt)
}
