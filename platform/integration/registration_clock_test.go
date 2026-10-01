package integration_test

import (
	"context"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/destination"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
	"github.com/complynx/zns-chatbot/platform/internal/registrationingress"
	"github.com/complynx/zns-chatbot/platform/internal/registrationnative"
)

type registrationClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *registrationClock) Now(context.Context) (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now, nil
}

func (c *registrationClock) advance(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

func TestRegistrationClockOuterDeliveryWaitRollsBack(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"solo", "invite", "assignment", "passport", "deadline", "runtime-batch", "derived-batch", "cancel", "sql-failure"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testRegistrationClockOuterDeliveryWait(t, scenario)
		})
	}
}

func testRegistrationClockOuterDeliveryWait(t *testing.T, scenario string) {
	t.Helper()
	db, service := bookingFixture(t)
	var start time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
	clock := &registrationClock{now: start}
	service.RegistrationClock = clock
	finish := start.Add(59 * time.Hour)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET finishes_at=$1`, finish)
	require.NoError(t, err)
	command := bookingCommand(scenario, "outer-clock-"+scenario, passbooking.Booking{})
	if scenario == "cancel" || scenario == "sql-failure" {
		command.Name = "solo"
	}
	if scenario == "sql-failure" {
		_, err = db.Exec(
			t.Context(),
			`CREATE FUNCTION core.clock_failed_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN RAISE EXCEPTION 'synthetic receipt failure'; END $$;
 CREATE TRIGGER clock_failed_receipt BEFORE INSERT ON core.pass_booking_operations FOR EACH ROW EXECUTE FUNCTION core.clock_failed_receipt()`,
		)
		require.NoError(t, err)
	}
	if scenario == "invite" {
		command.InviteTelegramID = 202
	}
	seedRegistrationClockMaintenance(t, db, service, clock, start, scenario)
	price := 0
	assignment := passbooking.AdminAssignment{Event: "dance", Key: command.Key, Target: "alice", TotalPrice: &price,
		Create: &passbooking.AdminCreate{FromProfile: true}}
	batch := passbooking.RuntimeBatch{
		Event:      "dance",
		Key:        command.Key,
		Action:     "admin_assign",
		Recipients: []int64{101},
		Options: passbooking.AdminAssignment{
			TotalPrice: &price,
			Create:     &passbooking.AdminCreate{FromProfile: true},
		},
	}
	var items []passbooking.RuntimeBatchItem
	run := func(ctx context.Context) error {
		switch scenario {
		case "runtime-batch":
			var runErr error
			items, runErr = service.RunBatch(ctx, "bob", batch)
			return runErr
		case "derived-batch":
			generation := int64(0)
			var runErr error
			items, runErr = (derivedmutation.Service{DB: db, Registration: service}).RunPassBatch(ctx, "bob", batch,
				readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}})
			return runErr
		case "assignment":
			_, runErr := service.AdminAssign(ctx, "bob", assignment)
			return runErr
		case "passport":
			_, runErr := service.ProcessPassportReminders(ctx)
			return runErr
		case "deadline":
			_, runErr := service.ProcessDeadlines(ctx)
			return runErr
		default:
			_, runErr := service.Execute(ctx, "alice", command)
			return runErr
		}
	}
	before := registrationClockTransactionRows(t, db)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.delivery_lanes(bot_id,chat) VALUES($1,'101') ON CONFLICT DO NOTHING`,
		service.Delivery.BotID,
	)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	_, err = blocker.Exec(
		ctx,
		`SELECT next_sequence FROM core.delivery_lanes WHERE bot_id=$1 AND chat='101' FOR UPDATE`,
		service.Delivery.BotID,
	)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- run(ctx) }()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event_type='Lock' AND query LIKE '%core.delivery_lanes%FOR UPDATE%')`).Scan(&waiting)
		return readErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "domain writes reached the late delivery lane lock")
	if scenario == "cancel" {
		cancel()
		require.NoError(t, blocker.Rollback(context.WithoutCancel(t.Context())))
	} else {
		clock.advance(finish)
		require.NoError(t, blocker.Commit(ctx))
	}
	select {
	case err = <-done:
		requireRegistrationClockWaitError(t, scenario, err)
	case <-time.After(5 * time.Second):
		t.Fatal("outer transaction did not finish after lane release")
	}
	require.Equal(
		t,
		before,
		registrationClockTransactionRows(t, db),
		"all domain state, receipts and once markers rolled back",
	)
	if scenario == "cancel" {
		return
	}
	if scenario == "runtime-batch" || scenario == "derived-batch" {
		if scenario == "derived-batch" {
			require.Len(t, items, 1)
			require.Equal(t, passbooking.AdminBatchNotAttempted, items[0].Outcome.Status)
		} else {
			require.Empty(t, items, "existing runtime wrapper returns only completed earlier items")
		}
		stored, readErr := service.ReadRuntimeBatch(ctx, "bob", batch)
		require.NoError(t, readErr)
		require.Equal(t, passbooking.AdminBatchNotAttempted, stored.Items()[0].Outcome.Status)
		require.NoError(t, run(ctx))
		require.Equal(t, passbooking.AdminBatchRejected, items[0].Outcome.Status)
		require.Equal(t, "pass_event_finished", items[0].Outcome.Code)
		return
	}
	switch scenario {
	case "assignment":
		requireCode(t, run(ctx), "pass_event_finished")
	case "invite", "solo", "sql-failure":
		requireCode(t, run(ctx), "pass_sales_closed")
	default:
		require.NoError(t, run(ctx), "maintenance retries at current time without retaining stale markers")
	}
}

func seedRegistrationClockMaintenance(
	t *testing.T,
	db *pgxpool.Pool,
	service passbooking.Service,
	clock *registrationClock,
	start time.Time,
	scenario string,
) {
	t.Helper()
	var err error
	if scenario == "passport" || scenario == "deadline" {
		seed := bookingCommand("solo", "outer-seed", passbooking.Booking{})
		if scenario == "deadline" {
			seed.Name, seed.InviteTelegramID = "invite", 202
		}
		_, err = service.Execute(t.Context(), "alice", seed)
		require.NoError(t, err)
		if scenario == "passport" {
			_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true`)
			require.NoError(t, err)
		}
		if scenario == "deadline" {
			clock.advance(start.Add(58*time.Hour + time.Microsecond))
		}
	}
}

func requireRegistrationClockWaitError(t *testing.T, scenario string, err error) {
	t.Helper()
	switch scenario {
	case "cancel":
		require.ErrorIs(t, err, context.Canceled)
	case "sql-failure":
		require.ErrorIs(t, err, core.ErrDatabase)
		require.True(t, core.IsDatabaseFailure(err))
		require.NotErrorIs(t, err, passbooking.ErrRegistrationTimeChanged)
	default:
		require.ErrorIs(t, err, passbooking.ErrRegistrationTimeChanged)
	}
}

func registrationClockTransactionRows(t *testing.T, db *pgxpool.Pool) string {
	t.Helper()
	var rows string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT jsonb_build_array(
 (SELECT COALESCE(jsonb_agg(to_jsonb(b) ORDER BY event_id,owner),'[]') FROM core.pass_bookings b),
 (SELECT COALESCE(jsonb_agg(to_jsonb(n) ORDER BY id),'[]') FROM core.pass_notifications n),
 (SELECT COALESCE(jsonb_agg(to_jsonb(o) ORDER BY event_id,actor,key_hash),'[]') FROM core.pass_booking_operations o),
 (SELECT COALESCE(jsonb_agg(to_jsonb(r) ORDER BY owner),'[]') FROM core.pass_passport_reminders r),
 (SELECT COALESCE(jsonb_agg(to_jsonb(m) ORDER BY event_id,owner),'[]') FROM core.pass_deadline_markers m))::text`).Scan(&rows))
	return rows
}

func TestRegistrationClockPassportLaterProfileWaitRollsBackEarlierMarker(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	var start time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
	clock := &registrationClock{now: start}
	service.RegistrationClock = clock
	for _, owner := range []string{"alice", "bob"} {
		_, err := service.Execute(
			t.Context(),
			owner,
			bookingCommand("solo", "clock-passport-seed-"+owner, passbooking.Booking{}),
		)
		require.NoError(t, err)
	}
	finish := start.Add(time.Hour)
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true,finishes_at=$1`, finish)
	require.NoError(t, err)
	before := registrationClockTransactionRows(t, db)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	_, err = blocker.Exec(ctx, `SELECT passport FROM core.pass_profiles WHERE owner='bob' FOR UPDATE`)
	require.NoError(t, err)
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		count, runErr := service.ProcessPassportReminders(ctx)
		done <- result{count: count, err: runErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event_type='Lock' AND query='SELECT passport FROM core.pass_profiles WHERE owner=$1 FOR SHARE')`).Scan(&waiting)
		return readErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond)
	clock.advance(finish)
	require.NoError(t, blocker.Commit(ctx))
	outcome := <-done
	require.ErrorIs(t, outcome.err, passbooking.ErrRegistrationTimeChanged)
	require.Zero(t, outcome.count)
	require.Equal(
		t,
		before,
		registrationClockTransactionRows(t, db),
		"an earlier user's once marker cannot survive a later profile wait",
	)
	count, err := service.ProcessPassportReminders(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRegistrationClockInvitationMaintenanceBoundary(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	clock := &registrationClock{}
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&clock.now))
	service.RegistrationClock = clock
	command := bookingCommand("invite", "clock-invite", passbooking.Booking{})
	command.InviteTelegramID = 202
	booking, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	require.Equal(t, "waiting-for-couple", booking.State)
	require.WithinDuration(t, clock.now, *booking.InvitationStartedAt, 0)
	start := *booking.InvitationStartedAt
	clock.advance(start.Add(58 * time.Hour))
	count, err := service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
	unchanged, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.Equal(t, booking.Version, unchanged.Version)
	clock.advance(start.Add(58*time.Hour + time.Microsecond))
	count, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	require.Positive(t, count, "the invitation alone must select this event")
	expired, err := service.Get(t.Context(), "alice", "dance")
	require.NoError(t, err)
	require.NotEqual(t, "waiting-for-couple", expired.State)
	require.Nil(t, expired.InvitationStartedAt)
	require.Zero(t, expired.InvitationTarget)
	require.Greater(t, expired.Version, booking.Version)
	count, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestRegistrationClockNewIngressAndExactTurnExpiry(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	clock := &registrationClock{}
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&clock.now))
	start := clock.now
	service.RegistrationClock = clock
	command := bookingCommand("solo", "clock-native", passbooking.Booking{})
	bind, err := (registrationnative.Envelope{
		Owner: "alice", Chat: 101, Token: "clock-native", Revision: 1, Command: command,
	}).Binding()
	require.NoError(t, err)
	ref := registrationingress.Reference{BotID: 7, UpdateID: 20}
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	ctx := registrationingress.WithClock(t.Context(), clock)
	require.NoError(t, registrationingress.SaveClassifiedTelegram(ctx, tx, ref, 101, bind))
	require.NoError(t, tx.Commit(t.Context()))
	first, err := service.CaptureAdmission(
		t.Context(),
		"alice",
		passbooking.AdmissionRequest{Command: command, Ingress: &ref},
	)
	require.NoError(t, err)
	clock.advance(start.Add(time.Minute))
	second, err := service.CaptureAdmission(t.Context(), "bob", passbooking.AdmissionRequest{
		Command: bookingCommand("solo", "clock-application", passbooking.Booking{}),
	})
	require.NoError(t, err)
	var received, checked, deadline time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT g.received_at,i.checked_at,i.turn_expires_at
 FROM core.registration_intents i JOIN core.registration_ingress g ON g.id=i.ingress_id WHERE i.id=$1`, second.ID).
		Scan(&received, &checked, &deadline))
	require.Equal(t, start.Add(time.Minute), received)
	require.Equal(t, received, checked)
	require.Equal(t, received.Add(10*time.Minute), deadline)
	clock.advance(start.Add(10*time.Minute - time.Microsecond))
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	var position int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT effective_position FROM core.registration_intents WHERE id=$1`, first.ID).
			Scan(&position),
	)
	require.Equal(t, first.Position, position)
	clock.advance(start.Add(10 * time.Minute))
	_, err = service.ProcessDeadlines(t.Context())
	require.NoError(t, err)
	var original, requeues int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT ingress_id,effective_position,requeue_count FROM core.registration_intents WHERE id=$1`, first.ID).
			Scan(&original, &position, &requeues),
	)
	require.Equal(t, first.Position, original)
	require.Greater(t, position, second.Position)
	require.EqualValues(t, 1, requeues)
	tx, err = db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	require.NoError(t, registrationingress.SaveClassifiedTelegram(ctx, tx, ref, 101, bind))
	require.NoError(t, tx.Commit(t.Context()))
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT received_at FROM core.registration_ingress WHERE id=$1`, first.Position).
			Scan(&received),
	)
	require.Equal(t, start, received, "replay never restamps native evidence")
}

func TestRegistrationClockPreparedCommandObservesAfterPrepare(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	clock := &registrationClock{}
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&clock.now))
	service.RegistrationClock = clock
	command := bookingCommand("solo", "clock-prepared", passbooking.Booking{})
	_, err := service.CaptureAdmission(t.Context(), "alice", passbooking.AdmissionRequest{Command: command})
	require.NoError(t, err)
	tx, err := db.Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	prepared, err := service.PrepareInTx(t.Context(), tx, "alice", command)
	require.NoError(t, err)
	observed := clock.now.Add(time.Minute)
	clock.advance(observed)
	booking, err := prepared.Apply(t.Context())
	require.NoError(t, err)
	require.WithinDuration(t, observed, booking.CreatedAt, 0)
	require.WithinDuration(t, observed, *booking.AssignedAt, 0)
	require.NoError(t, tx.Commit(t.Context()))
}

func TestRegistrationClockApplicationAllocatorWait(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	clock := &registrationClock{}
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&clock.now))
	start := clock.now
	service.RegistrationClock = clock
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	_, err = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(782619)`)
	require.NoError(t, err)
	type captured struct {
		admission passbooking.Admission
		err       error
	}
	result := make(chan captured, 1)
	go func() {
		admission, captureErr := service.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{
			Command: bookingCommand("solo", "clock-allocator-wait", passbooking.Booking{}),
		})
		result <- captured{admission: admission, err: captureErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		queryErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event='advisory' AND query='SELECT pg_advisory_xact_lock(782619)')`).
			Scan(&waiting)
		return queryErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "real capture waits on the allocator")
	advanced := start.Add(11 * time.Minute)
	clock.advance(advanced)
	require.NoError(t, blocker.Commit(ctx))
	var outcome captured
	select {
	case outcome = <-result:
	case <-ctx.Done():
		t.Fatal("capture did not complete after allocator release")
	}
	require.ErrorIs(t, outcome.err, passbooking.ErrRegistrationTimeChanged)
	require.Zero(t, outcome.admission.ID)
	var rolledBack int
	require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM core.registration_intents`).Scan(&rolledBack))
	require.Zero(t, rolledBack)
	outcome.admission, outcome.err = service.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{
		Command: bookingCommand("solo", "clock-allocator-wait", passbooking.Booking{}),
	})
	require.NoError(t, outcome.err)
	var received, checked, deadline time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT g.received_at,i.checked_at,i.turn_expires_at
 FROM core.registration_intents i JOIN core.registration_ingress g ON g.id=i.ingress_id WHERE i.id=$1`, outcome.admission.ID).
		Scan(&received, &checked, &deadline))
	require.WithinDuration(t, advanced, received, 0)
	require.WithinDuration(t, advanced, checked, 0)
	require.WithinDuration(t, received.Add(10*time.Minute), deadline, 0)
	clock.advance(advanced.Add(time.Minute))
	replayed, err := service.CaptureAdmission(ctx, "alice", passbooking.AdmissionRequest{
		Command: bookingCommand("solo", "clock-allocator-wait", passbooking.Booking{}),
	})
	require.NoError(t, err)
	require.Equal(t, outcome.admission.ID, replayed.ID)
	var unchanged time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT checked_at FROM core.registration_intents WHERE id=$1`, replayed.ID).
		Scan(&unchanged))
	require.WithinDuration(t, checked, unchanged, 0, "existing admission is never restamped")
	var key string
	require.NoError(
		t,
		db.QueryRow(ctx, `SELECT request_key FROM core.registration_ingress WHERE id=$1`, replayed.Position).
			Scan(&key),
	)
	tx, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
	position, firstReceived, err := registrationingress.ApplicationObservation(
		registrationingress.WithClock(ctx, clock), tx, "alice", key,
	)
	require.NoError(t, err)
	require.Equal(t, replayed.Position, position)
	require.WithinDuration(t, received, firstReceived, 0, "allocator conflict returns immutable first reception")
	require.NoError(t, tx.Commit(ctx))
}

func TestRegistrationClockTurnRotationWait(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"second-expiry-sales", "event-finish", "admin-finish", "maintenance-finish"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, service := bookingFixture(t)
			clock := &registrationClock{}
			require.NoError(t, db.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&clock.now))
			start := clock.now
			service.RegistrationClock = clock
			if scenario == "second-expiry-sales" {
				_, err := db.Exec(
					t.Context(),
					"UPDATE core.pass_event_tiers SET starts_at=$1",
					start.Add(11*time.Minute),
				)
				require.NoError(t, err)
			} else {
				_, err := db.Exec(t.Context(), "UPDATE core.pass_events SET finishes_at=$1", start.Add(11*time.Minute))
				require.NoError(t, err)
			}
			first, err := service.CaptureAdmission(t.Context(), "alice", passbooking.AdmissionRequest{
				Command: bookingCommand("solo", "clock-rotate-alice", passbooking.Booking{}),
			})
			require.NoError(t, err)
			clock.advance(start.Add(time.Minute))
			command := bookingCommand("solo", "clock-rotate-bob", passbooking.Booking{})
			second, err := service.CaptureAdmission(t.Context(), "bob", passbooking.AdmissionRequest{Command: command})
			require.NoError(t, err)
			clock.advance(start.Add(10 * time.Minute))
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			tx, err := db.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tx.Rollback(context.WithoutCancel(t.Context())) })
			var apply func(context.Context) error
			switch scenario {
			case "admin-finish":
				prepared, prepareErr := service.PrepareAssignmentInTx(ctx, tx, "bob", passbooking.AdminAssignment{
					Event: "dance", Key: "clock-rotation-admin", Target: "alice",
				})
				require.NoError(t, prepareErr)
				apply = func(ctx context.Context) error { _, applyErr := prepared.Apply(ctx); return applyErr }
			case "maintenance-finish":
				require.NoError(t, tx.Rollback(ctx))
				apply = func(ctx context.Context) error { _, applyErr := service.ProcessDeadlines(ctx); return applyErr }
			default:
				prepared, prepareErr := service.PrepareInTx(ctx, tx, "bob", command)
				require.NoError(t, prepareErr)
				apply = func(ctx context.Context) error { _, applyErr := prepared.Apply(ctx); return applyErr }
			}
			blocker, err := db.Begin(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
			_, err = blocker.Exec(ctx, "SELECT pg_advisory_xact_lock(782619)")
			require.NoError(t, err)
			result := make(chan error, 1)
			go func() { result <- apply(ctx) }()
			require.Eventually(t, func() bool {
				var waiting bool
				queryErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event='advisory' AND query='SELECT pg_advisory_xact_lock(782619)')`).Scan(&waiting)
				return queryErr == nil && waiting
			}, 5*time.Second, 10*time.Millisecond, "prepared effect waits on turn rotation, not application admission")
			advanced := start.Add(12 * time.Minute)
			clock.advance(advanced)
			require.NoError(t, blocker.Commit(ctx))
			select {
			case err = <-result:
			case <-ctx.Done():
				t.Fatal("rotation did not complete after allocator release")
			}
			switch scenario {
			case "event-finish":
				requireCode(t, err, "pass_sales_closed")
			case "admin-finish":
				requireCode(t, err, "pass_event_finished")
			case "maintenance-finish":
				require.NoError(t, err)
			default:
				require.NoError(t, err)
				require.NoError(t, tx.Commit(ctx))
				for _, admission := range []passbooking.Admission{first, second} {
					var position, rotations int64
					var deadline time.Time
					require.NoError(t, db.QueryRow(ctx, `SELECT effective_position,requeue_count,turn_expires_at
 FROM core.registration_intents WHERE id=$1`, admission.ID).Scan(&position, &rotations, &deadline))
					require.Greater(t, position, second.Position)
					require.EqualValues(t, 1, rotations, "both expired turns were selected after wait")
					require.WithinDuration(t, advanced.Add(10*time.Minute), deadline, 0)
				}
				booking, getErr := service.Get(ctx, "bob", "dance")
				require.NoError(t, getErr)
				require.Positive(t, booking.Version, "sales opened while allocator blocked")
				require.WithinDuration(t, advanced, booking.CreatedAt, 0)
			}
		})
	}
}

func TestRegistrationClockAnnouncementsAndPassportEligibility(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"active-before-wall", "finished-before-wall", "recovery-before-wall"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			db, service := bookingFixture(t)
			var wall time.Time
			require.NoError(t, db.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&wall))
			start := wall
			if scenario == "active-before-wall" {
				start = start.Add(-48 * time.Hour)
			}
			finish := start.Add(time.Hour)
			clock := &registrationClock{now: start}
			service.RegistrationClock = clock
			service.AnnouncementBindings = &destination.Bindings{}
			_, err := db.Exec(
				t.Context(),
				"UPDATE core.pass_events SET finishes_at=$1,thread_channel='@clock',thread_id=42",
				finish,
			)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), "UPDATE core.pass_event_tiers SET starts_at=$1", start.Add(-time.Hour))
			require.NoError(t, err)
			resolved := []string{}
			resolver := publicationResolver(func(_ context.Context, channel string) (int64, error) {
				resolved = append(resolved, channel)
				return -100123, nil
			})
			require.NoError(t, service.RefreshAnnouncementDestinations(t.Context(), resolver, time.Minute))
			require.Equal(t, []string{"@clock"}, resolved)
			_, err = service.Execute(
				t.Context(),
				"alice",
				bookingCommand("solo", "clock-announcement", passbooking.Booking{}),
			)
			require.NoError(t, err)
			item, found, err := service.ClaimRegistrationAnnouncement(t.Context())
			require.NoError(t, err)
			require.True(t, found, "domain-live event is enqueued and survives recovery even when wall-expired")
			var lease time.Time
			require.NoError(
				t,
				db.QueryRow(t.Context(), "SELECT lease_until FROM core.pass_registration_announcements WHERE id=$1", item.ID).
					Scan(&lease),
			)
			require.WithinDuration(t, wall.Add(2*time.Minute), lease, 10*time.Second)
			_, err = db.Exec(
				t.Context(),
				"UPDATE core.pass_bookings SET state='assigned',assigned_at=$1,price=100 WHERE owner='alice'",
				start,
			)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), "UPDATE core.pass_events SET passport_required=true")
			require.NoError(t, err)
			drainPassDomainNotices(t, service)
			count, err := service.ProcessPassportReminders(t.Context())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			var noticeID int64
			require.NoError(
				t,
				db.QueryRow(t.Context(), "SELECT id FROM core.pass_notifications WHERE kind='passport_required' AND owner='alice'").
					Scan(&noticeID),
			)
			notice, found, err := service.PrepareNotification(t.Context(), noticeID)
			require.NoError(t, err)
			require.True(t, found)
			require.True(t, notice.Current)
			if scenario == "active-before-wall" {
				gate, beginErr := service.BeginRegistrationAnnouncement(
					t.Context(),
					delivery.Attempt{ID: item.ID, Generation: item.Attempts},
				)
				require.NoError(t, beginErr)
				require.True(
					t,
					gate.Ready,
					"generated current predicate uses domain time while lease remains wall time",
				)
				return
			}
			clock.advance(finish.Add(time.Microsecond))
			if scenario == "recovery-before-wall" {
				require.NoError(t, service.RecoverRegistrationAnnouncements(t.Context()))
				var state string
				require.NoError(
					t,
					db.QueryRow(t.Context(), "SELECT state FROM core.pass_registration_announcements WHERE id=$1", item.ID).
						Scan(&state),
				)
				require.Equal(t, "cancelled", state)
				return
			}
			resolved = nil
			require.NoError(t, service.RefreshAnnouncementDestinations(t.Context(), resolver, time.Minute))
			require.Empty(t, resolved)
			gate, err := service.BeginRegistrationAnnouncement(
				t.Context(),
				delivery.Attempt{ID: item.ID, Generation: item.Attempts},
			)
			require.NoError(t, err)
			require.False(t, gate.Ready)
			require.Equal(t, "announcement_superseded", gate.Reason)
			reminder, err := service.BeginNotification(
				t.Context(),
				passbooking.NotificationAttempt{
					ID: notice.ID, Generation: notice.DeliveryAttempt,
					Wire: notificationTestWire(),
				},
			)
			require.NoError(t, err)
			require.False(t, reminder.Ready)
			require.Equal(t, "notification_no_longer_current", reminder.Reason)
			// A second assigned owner remains unmarked after domain finish although wall time is still before finish.
			_, err = db.Exec(
				t.Context(),
				"INSERT INTO core.pass_bookings(event_id,owner,version,state,role,kind,payment_admin,created_at,assigned_at,price) VALUES('dance','bob',1,'assigned','follower','solo','bob',$1,$1,100)",
				start,
			)
			require.NoError(t, err)
			count, err = service.ProcessPassportReminders(t.Context())
			require.NoError(t, err)
			require.Zero(t, count)
			var marked bool
			require.NoError(
				t,
				db.QueryRow(t.Context(), "SELECT EXISTS(SELECT 1 FROM core.pass_passport_reminders WHERE owner='bob')").
					Scan(&marked),
			)
			require.False(t, marked)
		})
	}
}

func TestRegistrationClockNotificationLockWait(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"announcement-attempt", "passport-profile", "delivery-lane"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testRegistrationNotificationLockWait(t, name)
		})
	}
}

func testRegistrationNotificationLockWait(t *testing.T, scenario string) {
	t.Helper()
	db, service := bookingFixture(t)
	var start time.Time
	require.NoError(t, db.QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&start))
	finish := start.Add(time.Hour)
	clock := &registrationClock{now: start}
	service.RegistrationClock = clock
	service.AnnouncementBindings = &destination.Bindings{}
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=$1,thread_channel='@clock',thread_id=42`,
		finish,
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_event_tiers SET starts_at=$1`, start.Add(-time.Hour))
	require.NoError(t, err)
	resolver := publicationResolver(func(context.Context, string) (int64, error) { return -100123, nil })
	require.NoError(t, service.RefreshAnnouncementDestinations(t.Context(), resolver, time.Minute))
	_, err = service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "clock-notification-wait", passbooking.Booking{}),
	)
	require.NoError(t, err)
	item, found, err := service.ClaimRegistrationAnnouncement(t.Context())
	require.NoError(t, err)
	require.True(t, found)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='assigned',assigned_at=$1,price=100 WHERE owner='alice'`,
		start,
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	queryPattern := "%core.pass_registration_announcements%FOR UPDATE%"
	switch scenario {
	case "passport-profile":
		_, err = blocker.Exec(ctx, `SELECT passport FROM core.pass_profiles WHERE owner='alice' FOR UPDATE`)
		queryPattern = "SELECT passport FROM core.pass_profiles WHERE owner=$1 FOR SHARE"
	case "delivery-lane":
		_, err = blocker.Exec(
			ctx,
			`SELECT l.chat FROM core.delivery_lanes l JOIN core.delivery_queue q USING(bot_id,chat)
 WHERE q.bot_id=$1 AND q.owner_kind='announcement' AND q.owner_key=$2 FOR UPDATE OF l`,
			service.Delivery.BotID,
			strconv.FormatInt(item.ID, 10),
		)
		queryPattern = "%core.delivery_lanes%FOR UPDATE%"
	default:
		_, err = blocker.Exec(
			ctx,
			`SELECT id FROM core.pass_registration_announcements WHERE id=$1 FOR UPDATE`,
			item.ID,
		)
	}
	require.NoError(t, err)
	type result struct {
		count int
		gate  delivery.Admission
		err   error
	}
	done := make(chan result, 1)
	go func() {
		if scenario == "passport-profile" {
			count, runErr := service.ProcessPassportReminders(ctx)
			done <- result{count: count, err: runErr}
			return
		}
		gate, runErr := service.BeginRegistrationAnnouncement(
			ctx,
			delivery.Attempt{ID: item.ID, Generation: item.Attempts},
		)
		done <- result{gate: gate, err: runErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, queryPattern).Scan(&waiting)
		return readErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "actual profile or attempt lock blocks eligibility admission")
	var proveRetainedLocks func()
	if scenario == "delivery-lane" {
		proveRetainedLocks = holdAnnouncementCancellationLane(ctx, t, db, service.Delivery.BotID, item.ID)
	}
	clock.advance(finish.Add(time.Microsecond))
	require.NoError(t, blocker.Commit(ctx))
	if proveRetainedLocks != nil {
		proveRetainedLocks()
	}
	var outcome result
	select {
	case outcome = <-done:
	case <-ctx.Done():
		t.Fatal("notification did not finish after lock release")
	}
	if scenario != "announcement-attempt" {
		require.ErrorIs(t, outcome.err, passbooking.ErrRegistrationTimeChanged)
		if scenario == "passport-profile" {
			outcome.count, outcome.err = service.ProcessPassportReminders(ctx)
		} else {
			outcome.gate, outcome.err = service.BeginRegistrationAnnouncement(ctx,
				delivery.Attempt{ID: item.ID, Generation: item.Attempts})
		}
	}
	require.NoError(t, outcome.err)
	if scenario == "passport-profile" {
		require.Zero(t, outcome.count)
		var markers, notices int
		require.NoError(
			t,
			db.QueryRow(ctx, `SELECT count(*) FROM core.pass_passport_reminders WHERE owner='alice'`).Scan(&markers),
		)
		require.NoError(
			t,
			db.QueryRow(ctx, `SELECT count(*) FROM core.pass_notifications WHERE owner='alice' AND kind='passport_required'`).
				Scan(&notices),
		)
		require.Zero(t, markers, "expired eligibility cannot consume the permanent once marker")
		require.Zero(t, notices)
		return
	}
	require.False(t, outcome.gate.Ready)
	require.Equal(t, "announcement_superseded", outcome.gate.Reason)
	var state, queueState string
	require.NoError(t, db.QueryRow(ctx, `SELECT a.state,q.state FROM core.pass_registration_announcements a
 JOIN core.delivery_queue q ON q.bot_id=a.bot_id AND q.owner_kind='announcement' AND q.owner_key=a.id::text
 WHERE a.id=$1`, item.ID).Scan(&state, &queueState))
	require.Equal(t, string(delivery.Cancelled), state)
	require.Equal(t, string(delivery.Cancelled), queueState)
	var grants, pacing int64
	require.NoError(t, db.QueryRow(ctx, `SELECT COALESCE((SELECT grants FROM core.delivery_fairness WHERE bot_id=$1),0),
 (SELECT count(*) FROM core.delivery_pacing WHERE bot_id=$1)`, service.Delivery.BotID).Scan(&grants, &pacing))
	require.Zero(t, grants, "expired admission must not consume a fairness grant")
	require.Zero(t, pacing, "expired admission must not retain pacing reservations")
}

func holdAnnouncementCancellationLane(ctx context.Context, t *testing.T, db *pgxpool.Pool, botID, itemID int64) func() {
	t.Helper()
	return holdClockCancellationLane(ctx, t, db, botID,
		delivery.Reference{Owner: delivery.Announcement, Key: strconv.FormatInt(itemID, 10)},
		[]clockAdmissionLock{
			{`SELECT id FROM core.pass_events WHERE id=$1 FOR UPDATE NOWAIT`, "dance"},
			{`SELECT owner FROM core.pass_bookings WHERE owner=$1 FOR UPDATE NOWAIT`, "alice"},
			{`SELECT id FROM core.pass_registration_announcements WHERE id=$1 FOR UPDATE NOWAIT`, itemID},
		})
}

type clockAdmissionLock struct {
	query string
	key   any
}

// A second lane waiter holds cancellation after reservation rollback while
// NOWAIT probes check that the original authority/attempt locks survived.
func holdClockCancellationLane(
	ctx context.Context,
	t *testing.T,
	db *pgxpool.Pool,
	botID int64,
	ref delivery.Reference,
	locks []clockAdmissionLock,
) func() {
	t.Helper()
	guard, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = guard.Rollback(context.WithoutCancel(t.Context())) })
	ready := make(chan error, 1)
	go func() {
		_, lockErr := guard.Exec(
			ctx,
			`SELECT l.chat FROM core.delivery_lanes l JOIN core.delivery_queue q USING(bot_id,chat)
 WHERE q.bot_id=$1 AND q.owner_kind=$2 AND q.owner_key=$3 FOR UPDATE OF l`,
			botID,
			string(ref.Owner),
			ref.Key,
		)
		ready <- lockErr
	}()
	require.Eventually(t, func() bool {
		var count int
		readErr := db.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database()
 AND wait_event_type='Lock' AND query LIKE '%core.delivery_lanes%FOR UPDATE%'`).Scan(&count)
		return readErr == nil && count >= 2
	}, 5*time.Second, 10*time.Millisecond, "both admission and cancellation guard wait for the real lane")
	return func() {
		t.Helper()
		select {
		case lockErr := <-ready:
			require.NoError(t, lockErr)
		case <-ctx.Done():
			t.Fatal("cancellation guard did not acquire released lane")
		}
		for _, lock := range locks {
			probe, probeErr := guard.Begin(ctx)
			require.NoError(t, probeErr)
			_, probeErr = probe.Exec(ctx, lock.query, lock.key)
			var denied *pgconn.PgError
			require.ErrorAs(t, probeErr, &denied)
			require.Equal(t, "55P03", denied.Code, "outer source lock retained after transport reservation rollback")
			require.NoError(t, probe.Rollback(ctx))
		}
		require.NoError(t, guard.Commit(ctx))
	}
}

func TestRegistrationClockPassportAdmissionLockWait(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"attempt", "lane", "live"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			testClockPassportAdmission(t, scenario)
		})
	}
}

func testClockPassportAdmission(t *testing.T, scenario string) {
	t.Helper()
	db, service := bookingFixture(t)
	var start time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
	finish := start.Add(time.Hour)
	clock := &registrationClock{now: start}
	service.RegistrationClock = clock
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET finishes_at=$1`, finish)
	require.NoError(t, err)
	_, err = service.Execute(
		t.Context(),
		"alice",
		bookingCommand("solo", "passport-admission-wait", passbooking.Booking{}),
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`UPDATE core.pass_bookings SET state='assigned',assigned_at=$1,price=100 WHERE owner='alice'`,
		start,
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true`)
	require.NoError(t, err)
	drainPassDomainNotices(t, service)
	count, err := service.ProcessPassportReminders(t.Context())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	var id int64
	require.NoError(
		t,
		db.QueryRow(t.Context(), `SELECT id FROM core.pass_notifications WHERE owner='alice' AND kind='passport_required'`).
			Scan(&id),
	)
	notice, found, err := service.PrepareNotification(t.Context(), id)
	require.NoError(t, err)
	require.True(t, found)
	require.True(t, notice.Current)
	attempt := passbooking.NotificationAttempt{
		ID: id, Generation: notice.DeliveryAttempt,
		Wire: notificationTestWire(),
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	if scenario == "live" {
		gate, beginErr := service.BeginNotification(ctx, attempt)
		require.NoError(t, beginErr)
		require.True(t, gate.Ready, "configured live reminder retains admission")
		status, statusErr := service.NotificationStatus(ctx, id)
		require.NoError(t, statusErr)
		require.Equal(t, string(delivery.Sending), status.State)
		return
	}
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	queryPattern := "%core.pass_notifications%FOR UPDATE%"
	if scenario == "attempt" {
		_, err = blocker.Exec(ctx, `SELECT id FROM core.pass_notifications WHERE id=$1 FOR UPDATE`, id)
	} else {
		_, err = blocker.Exec(
			ctx,
			`SELECT l.chat FROM core.delivery_lanes l JOIN core.delivery_queue q USING(bot_id,chat)
 WHERE q.bot_id=$1 AND q.owner_kind='passes' AND q.owner_key=$2 FOR UPDATE OF l`,
			service.Delivery.BotID,
			strconv.FormatInt(id, 10),
		)
		queryPattern = "%core.delivery_lanes%FOR UPDATE%"
	}
	require.NoError(t, err)
	type result struct {
		gate passbooking.NotificationAdmission
		err  error
	}
	done := make(chan result, 1)
	go func() {
		gate, beginErr := service.BeginNotification(ctx, attempt)
		done <- result{gate: gate, err: beginErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, queryPattern).Scan(&waiting)
		return readErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "passport BeginNotification waits on the actual attempt or lane")
	var retained func()
	if scenario == "lane" {
		retained = holdClockCancellationLane(ctx, t, db, service.Delivery.BotID,
			delivery.Reference{Owner: delivery.Passes, Key: strconv.FormatInt(id, 10)},
			[]clockAdmissionLock{
				{`SELECT id FROM core.pass_events WHERE id=$1 FOR UPDATE NOWAIT`, "dance"},
				{`SELECT id FROM core.users WHERE id=$1 FOR UPDATE NOWAIT`, "alice"},
				{`SELECT id FROM core.pass_notifications WHERE id=$1 FOR UPDATE NOWAIT`, id},
			})
	}
	clock.advance(finish.Add(time.Microsecond))
	require.NoError(t, blocker.Commit(ctx))
	if retained != nil {
		retained()
	}
	var outcome result
	select {
	case outcome = <-done:
	case <-ctx.Done():
		t.Fatal("passport admission did not finish after lock release")
	}
	require.ErrorIs(t, outcome.err, passbooking.ErrRegistrationTimeChanged)
	require.False(t, outcome.gate.Ready)
	gate, err := service.BeginNotification(ctx, attempt)
	outcome.gate, outcome.err = gate, err
	require.NoError(t, outcome.err)
	require.False(t, outcome.gate.Ready)
	require.Equal(t, "notification_no_longer_current", outcome.gate.Reason)
	status, err := service.NotificationStatus(ctx, id)
	require.NoError(t, err)
	require.Equal(t, string(delivery.Cancelled), status.State)
	require.Equal(t, outcome.gate.Reason, status.Reason)
	require.Equal(t, attempt.Generation, status.Attempt)
	require.Zero(t, status.MessageID)
	require.Zero(t, status.FailureCount)
	var queue string
	var leaseCleared bool
	var grants, pacing, markers int64
	require.NoError(t, db.QueryRow(ctx, `SELECT q.state,n.lease_until IS NULL,
 COALESCE((SELECT grants FROM core.delivery_fairness WHERE bot_id=$1),0),
 (SELECT count(*) FROM core.delivery_pacing WHERE bot_id=$1),
 (SELECT count(*) FROM core.pass_passport_reminders WHERE owner='alice')
 FROM core.pass_notifications n JOIN core.delivery_queue q
 ON q.bot_id=n.bot_id AND q.owner_kind='passes' AND q.owner_key=n.id::text
 WHERE n.id=$2`, service.Delivery.BotID, id).Scan(&queue, &leaseCleared, &grants, &pacing, &markers))
	require.Equal(t, string(delivery.Cancelled), queue)
	require.True(t, leaseCleared)
	require.Zero(t, grants, "expired passport admission consumes no fairness grant")
	require.Zero(t, pacing, "expired passport admission retains no pacing reservations")
	require.EqualValues(t, 1, markers, "admission does not change the committed once marker")
}

func TestRegistrationClockTwoCapturesRetainFirstDependency(t *testing.T) {
	t.Parallel()
	for _, advance := range []bool{false, true} {
		t.Run(strconv.FormatBool(advance), func(t *testing.T) {
			t.Parallel()
			db, service := bookingFixture(t)
			var start time.Time
			require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
			seedRegistrationClockSecondEvent(t, db, start)
			clock := &registrationClock{now: start}
			service.RegistrationClock = clock
			err := captureRegistrationClockPair(t.Context(), db, service, clock, start, advance)
			if advance {
				require.ErrorIs(t, err, passbooking.ErrRegistrationTimeChanged)
				var intents, requests, ingress int
				require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.registration_intents),
 (SELECT count(*) FROM core.registration_intent_requests),
 (SELECT count(*) FROM core.registration_ingress)`).Scan(&intents, &requests, &ingress))
				require.Zero(t, intents)
				require.Zero(t, requests)
				require.Zero(t, ingress)
				err = captureRegistrationClockPair(t.Context(), db, service, clock, start, false)
			}
			require.NoError(t, err)
			var count, current int
			now, clockErr := clock.Now(t.Context())
			require.NoError(t, clockErr)
			require.NoError(t, db.QueryRow(t.Context(), `SELECT count(*),count(*) FILTER(WHERE checked_at=$1)
 FROM core.registration_intents WHERE owner='alice'`, now).Scan(&count, &current))
			require.Equal(t, 2, count)
			require.Equal(t, 2, current, "both same-key captures commit only the current attempt's decisions")
		})
	}
}

func captureRegistrationClockPair(
	ctx context.Context, db *pgxpool.Pool, service passbooking.Service,
	clock *registrationClock, start time.Time, move bool,
) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	scoped, attempt := service.WithClockAttempt()
	for _, event := range []string{"dance", "dance-b"} {
		if move && event == "dance-b" {
			clock.advance(start.Add(time.Microsecond))
		}
		command := bookingCommand("solo", "two-capture-"+event, passbooking.Booking{})
		command.Event = event
		if _, err = scoped.CaptureAdmissionInTx(
			ctx,
			tx,
			"alice",
			passbooking.AdmissionRequest{Command: command},
		); err != nil {
			return err
		}
	}
	if err = attempt.Check(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func seedRegistrationClockSecondEvent(t *testing.T, db *pgxpool.Pool, start time.Time) {
	t.Helper()
	_, err := db.Exec(
		t.Context(),
		`UPDATE core.pass_events SET finishes_at=$1,display_order=0 WHERE id='dance'`,
		start.Add(time.Hour),
	)
	require.NoError(t, err)
	_, err = db.Exec(
		t.Context(),
		`INSERT INTO core.pass_events(id,finishes_at,display_order) VALUES('dance-b',$1,1)`,
		start.Add(2*time.Hour),
	)
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_event_tiers(event_id,position,amount,price,starts_at)
 VALUES('dance-b',0,20,100,$1)`, start.Add(-time.Hour))
	require.NoError(t, err)
	_, err = db.Exec(t.Context(), `INSERT INTO core.pass_payment_admins(event_id,owner) VALUES('dance-b','bob')`)
	require.NoError(t, err)
}

func TestRegistrationClockPassportRetriesWholeEventSelection(t *testing.T) {
	t.Parallel()
	for _, advance := range []bool{false, true} {
		t.Run(strconv.FormatBool(advance), func(t *testing.T) {
			t.Parallel()
			testClockPassportEventSelection(t, advance)
		})
	}
}

func testClockPassportEventSelection(t *testing.T, advance bool) {
	t.Helper()
	db, service := bookingFixture(t)
	var start time.Time
	require.NoError(t, db.QueryRow(t.Context(), `SELECT clock_timestamp()`).Scan(&start))
	seedRegistrationClockSecondEvent(t, db, start)
	clock := &registrationClock{now: start}
	service.RegistrationClock = clock
	price := 0
	for _, event := range []string{"dance", "dance-b"} {
		_, err := service.AdminAssign(t.Context(), "bob", passbooking.AdminAssignment{
			Event: event, Key: "passport-selection-" + event, Target: "alice", TotalPrice: &price,
			Create: &passbooking.AdminCreate{FromProfile: true},
		})
		require.NoError(t, err)
	}
	_, err := db.Exec(t.Context(), `UPDATE core.pass_events SET passport_required=true`)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	blocker, err := db.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = blocker.Rollback(context.WithoutCancel(t.Context())) })
	_, err = blocker.Exec(ctx, `SELECT passport FROM core.pass_profiles WHERE owner='alice' FOR UPDATE`)
	require.NoError(t, err)
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		count, scanErr := service.ProcessPassportReminders(ctx)
		done <- result{count: count, err: scanErr}
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		readErr := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity
 WHERE datname=current_database() AND wait_event_type='Lock'
 AND query='SELECT passport FROM core.pass_profiles WHERE owner=$1 FOR SHARE')`).Scan(&waiting)
		return readErr == nil && waiting
	}, 5*time.Second, 10*time.Millisecond, "the DISTINCT ON selection completed before the real profile wait")
	if advance {
		clock.advance(start.Add(time.Hour + time.Microsecond))
	}
	require.NoError(t, blocker.Commit(ctx))
	var outcome result
	select {
	case outcome = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("passport scan did not finish after profile release")
	}
	event := "dance"
	if advance {
		require.ErrorIs(t, outcome.err, passbooking.ErrRegistrationTimeChanged)
		require.Zero(t, outcome.count)
		requireClockPassportSelection(t, db, "", 0)
		outcome.count, outcome.err = service.ProcessPassportReminders(ctx)
		event = "dance-b"
	}
	require.NoError(t, outcome.err)
	require.Equal(t, 1, outcome.count)
	requireClockPassportSelection(t, db, event, 1)
}

func requireClockPassportSelection(t *testing.T, db *pgxpool.Pool, event string, expected int) {
	t.Helper()
	var markers, notices int
	var selected string
	require.NoError(t, db.QueryRow(t.Context(), `SELECT
 (SELECT count(*) FROM core.pass_passport_reminders WHERE owner='alice'),
 count(*),COALESCE(min(event_id),'') FROM core.pass_notifications
 WHERE owner='alice' AND kind='passport_required'`).Scan(&markers, &notices, &selected))
	require.Equal(t, expected, markers)
	require.Equal(t, expected, notices)
	require.Equal(t, event, selected)
}
