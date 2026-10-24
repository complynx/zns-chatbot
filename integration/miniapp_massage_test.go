package integration_test

import (
	"context"
	"crypto/sha256"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func nativeMassageSession(
	t *testing.T,
	f paymentFixture,
	scope db.ProfileScope,
	telegramID int64,
) db.MiniAppMassageSession {
	t.Helper()
	return db.MiniAppMassageSession(nativeOrderSession(t, f, scope, telegramID))
}

func TestMiniAppMassageBothHandoffsAndReplayUseSingleConnection(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	session := nativeMassageSession(t, f, owner, 11002)
	deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	dsn, err := url.Parse(f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := dsn.Query()
	query.Set("pool_max_conns", "1")
	dsn.RawQuery = query.Encode()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	store, err := db.Open(ctx, dsn.String())
	require.NoError(t, err)
	defer store.Close()
	args := massageBookingArgs(f, schedule, 0)
	nativeOp := uuid.New()
	prepared, err := store.PrepareMiniAppMassageBooking(ctx, deployment, session, nativeOp, args)
	require.NoError(t, err)
	first, err := store.BookOwnMassage(ctx, owner, uuid.New(), prepared.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	recovered, err := store.BookMiniAppMassage(ctx, deployment, session, uuid.New(), prepared.ID, args)
	require.NoError(t, err)
	require.Equal(t, first, recovered)
	cancellation := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       first.ID,
		ExpectedVersion: first.Version,
	}
	cancelOp := uuid.New()
	cancelProposal, err := store.PrepareMiniAppMassageCancellation(ctx, deployment, session, cancelOp, cancellation)
	require.NoError(t, err)
	cancelled, err := store.CancelOwnMassage(ctx, owner, uuid.New(), cancelProposal.ID, cancellation)
	require.NoError(t, err)
	nativeCancelOp := uuid.New()
	recovered, err = store.CancelMiniAppMassage(
		ctx,
		deployment,
		session,
		nativeCancelOp,
		cancelProposal.ID,
		cancellation,
	)
	require.NoError(t, err)
	require.Equal(t, cancelled, recovered)
	original, err := store.BookMiniAppMassage(ctx, deployment, session, uuid.New(), prepared.ID, args)
	require.NoError(t, err)
	require.Equal(t, first, original, "Original book result survives cancellation")
	var source string
	require.NoError(
		t,
		store.Pool().
			QueryRow(ctx, "SELECT source FROM shuttle_massage_operations WHERE user_id=$1 AND opera" +
"tion_id=$2", owner.UserID, nativeCancelOp).
			Scan(&source),
	)
	require.Equal(t, "miniapp", source, "Cancellation provenance is independent of original booking source")
	require.Equal(t, db.MassageSourceAgent, first.Source)
	args = massageBookingArgs(f, schedule, 1)
	agentProposal, err := store.PrepareOwnMassageBooking(ctx, owner, uuid.New(), args)
	require.NoError(t, err)
	second, err := store.BookMiniAppMassage(ctx, deployment, session, uuid.New(), agentProposal.ID, args)
	require.NoError(t, err)
	require.NotEqual(t, first.ID, second.ID)
	require.Equal(t, db.MassageSourceMiniApp, second.Source)
	current, err := store.OwnMiniAppMassageBooking(ctx, deployment, session, f.Event.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, current)
	page, err := store.OwnMiniAppMassageBookings(ctx, deployment, session, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 2)
	receipt, err := store.OwnMiniAppMassageOperation(ctx, deployment, session, f.Event.ID, nativeCancelOp)
	require.NoError(t, err)
	require.Equal(t, cancelled, receipt.Result)
	_, err = store.OwnMiniAppMassageSchedule(ctx, deployment, session, f.Event.ID)
	require.NoError(t, err)
	events, err := store.OwnMiniAppMassageEvents(ctx, deployment, session, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, events.Events, 1)
	availability, err := store.OwnMiniAppMassageAvailability(
		ctx,
		deployment,
		session,
		massageAvailabilityArgs(f, schedule, "alex", 2, 2),
	)
	require.NoError(t, err)
	require.True(t, availability.Candidates[0].Available)
	require.EqualValues(t, 1, store.Pool().Config().MaxConns)
}

func TestMiniAppMassageRejectsInvalidSessionBeforeAllReplays(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "expiry", "generation", "foreign-owner", "foreign-organization", "revocation"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			assertMassageSessionDenied(t, kind)
		})
	}
}

func assertMassageSessionDenied(t *testing.T, kind string) {
	t.Helper()
	f, owner, other, schedule := massageBookingFixture(t, 2)
	ctx := t.Context()
	deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	session := nativeMassageSession(t, f, owner, 11002)
	operation := uuid.New()
	args := massageBookingArgs(f, schedule, 0)
	prepared, err := f.H.store.PrepareMiniAppMassageBooking(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	booking, err := f.H.store.BookMiniAppMassage(ctx, deployment, session, operation, prepared.ID, args)
	require.NoError(t, err)
	cancelOp := uuid.New()
	cancellation := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	cancelProposal, err := f.H.store.PrepareMiniAppMassageCancellation(ctx, deployment, session, cancelOp, cancellation)
	require.NoError(t, err)
	switch kind {
	case "logout":
		require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, owner.BotID, session.Bearer, session.Generation))
	case "expiry":
		_, err = f.H.store.Pool().
			Exec(ctx, "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interva" +
"l '1 second' WHERE bearer_hash=$1", session.Bearer[:])
		require.NoError(t, err)
	case "generation":
		session.Generation = sha256.Sum256([]byte("different-miniapp-generation"))
	case "foreign-owner":
		session = nativeMassageSession(t, f, other, 11003)
	case "foreign-organization":
		deployment.Organization = "another-organization"
	case "revocation":
		_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", owner.UserID)
		require.NoError(t, err)
	}
	_, err = f.H.store.PrepareMiniAppMassageBooking(ctx, deployment, session, operation, args)
	require.Error(t, err)
	_, err = f.H.store.BookMiniAppMassage(ctx, deployment, session, operation, prepared.ID, args)
	require.Error(t, err)
	_, err = f.H.store.PrepareMiniAppMassageCancellation(ctx, deployment, session, cancelOp, cancellation)
	require.Error(t, err)
	_, err = f.H.store.CancelMiniAppMassage(ctx, deployment, session, cancelOp, cancelProposal.ID, cancellation)
	require.Error(t, err)
	_, err = f.H.store.OwnMiniAppMassageBooking(ctx, deployment, session, f.Event.ID, booking.ID)
	require.Error(t, err)
	_, err = f.H.store.OwnMiniAppMassageOperation(ctx, deployment, session, f.Event.ID, operation)
	require.Error(t, err)
	if kind != "foreign-owner" {
		_, err = f.H.store.OwnMiniAppMassageBookings(ctx, deployment, session, f.Event.ID, uuid.Nil, 20)
		require.Error(t, err)
		_, err = f.H.store.OwnMiniAppMassageSchedule(ctx, deployment, session, f.Event.ID)
		require.Error(t, err)
		_, err = f.H.store.OwnMiniAppMassageEvents(ctx, deployment, session, uuid.Nil, 20)
		require.Error(t, err)
		_, err = f.H.store.OwnMiniAppMassageAvailability(
			ctx,
			deployment,
			session,
			massageAvailabilityArgs(f, schedule, "alex", 1, 2),
		)
		require.Error(t, err)
	}
	var state string
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT state FROM shuttle_massage_bookings WHERE id=$1", booking.ID).
			Scan(&state),
	)
	require.Equal(t, "booked", state)
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_operations").Scan(&count))
	require.Equal(t, 1, count)
}

func TestMiniAppMassageRechecksSessionAfterEventLockWait(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"logout", "revocation"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			assertMassageSessionAfterWait(t, kind)
		})
	}
}

func assertMassageSessionAfterWait(t *testing.T, kind string) {
	t.Helper()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	ctx := t.Context()
	deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	session := nativeMassageSession(t, f, owner, 11002)
	operation := uuid.New()
	args := massageBookingArgs(f, schedule, 0)
	prepared, err := f.H.store.PrepareMiniAppMassageBooking(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	blocking, err := f.H.store.Pool().Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = blocking.Rollback(ctx) }()
	_, err = blocking.Exec(ctx, "SELECT id FROM shuttle_events WHERE id=$1 FOR UPDATE", f.Event.ID)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() {
		_, e := f.H.store.BookMiniAppMassage(ctx, deployment, session, operation, prepared.ID, args)
		done <- e
	}()
	require.Eventually(t, func() bool {
		var waiting bool
		e := f.H.store.Pool().
			QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_datab" +
"ase() AND pid<>pg_backend_pid() AND $1::integer=ANY(pg_blocking_pids(pid" +
")))", blocking.Conn().PgConn().PID()).
			Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 5*time.Millisecond)
	if kind == "logout" {
		require.NoError(t, f.H.store.LogoutMiniAppSession(ctx, owner.BotID, session.Bearer, session.Generation))
	} else {
		_, err = f.H.store.Pool().Exec(ctx, "UPDATE shuttle_users SET revoked=true WHERE id=$1", owner.UserID)
		require.NoError(t, err)
	}
	require.NoError(t, blocking.Commit(ctx))
	select {
	case err = <-done:
		require.ErrorIs(t, err, db.ErrMiniAppDenied)
	case <-time.After(5 * time.Second):
		t.Fatal("Booking did not finish after event lock was released")
	}
	var count int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_bookings").Scan(&count))
	require.Zero(t, count)
}

func TestMiniAppMassageReceiptFailureRollsBackBookingAndProposalConsumption(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	ctx := t.Context()
	deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	session := nativeMassageSession(t, f, owner, 11002)
	operation := uuid.New()
	args := massageBookingArgs(f, schedule, 0)
	prepared, err := f.H.store.PrepareMiniAppMassageBooking(ctx, deployment, session, operation, args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(ctx, "CREATE FUNCTION fail_native_massage_receipt() RETURNS trigger LANGUAGE p" +
"lpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic massage receipt failure'; " +
"END $$;\n CREATE TRIGGER fail_native_massage_receipt BEFORE INSERT ON shu" +
"ttle_massage_operations FOR EACH ROW EXECUTE FUNCTION fail_native_massag" +
"e_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.BookMiniAppMassage(ctx, deployment, session, operation, prepared.ID, args)
	require.ErrorContains(t, err, "synthetic massage receipt failure")
	var bookings, operations int
	require.NoError(t, f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_bookings").Scan(&bookings))
	require.Zero(t, bookings)
	var reminders int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_reminders").Scan(&reminders),
	)
	require.Zero(t, reminders, "Receipt failure must roll back reminder intents with the booking")
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_operations").Scan(&operations),
	)
	require.Zero(t, operations)
	var consumed bool
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(ctx, "SELECT consumed_at IS NOT NULL FROM shuttle_massage_preparations WHERE id=$1", prepared.ID).
			Scan(&consumed),
	)
	require.False(t, consumed)
	_, err = f.H.store.Pool().
		Exec(ctx, "DROP TRIGGER fail_native_massage_receipt ON shuttle_massage_operations; " +
"DROP FUNCTION fail_native_massage_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.BookMiniAppMassage(ctx, deployment, session, operation, prepared.ID, args)
	require.NoError(t, err)
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(ctx, "SELECT count(*) FROM shuttle_massage_reminders").Scan(&reminders),
	)
	require.Equal(t, 2, reminders)
}
