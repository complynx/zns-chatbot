package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
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
