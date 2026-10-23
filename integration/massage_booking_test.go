package integration_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func massageBookingFixture(
	t *testing.T,
	tables int,
) (paymentFixture, db.ProfileScope, db.ProfileScope, db.MassageScheduleChange) {
	t.Helper()
	return massageBookingFixtureWithPeers(t, tables, newPeers(t))
}

func massageBookingFixtureWithPeers(
	t *testing.T,
	tables int,
	p *peers,
) (paymentFixture, db.ProfileScope, db.ProfileScope, db.MassageScheduleChange) {
	t.Helper()
	f := setupPayment(t, p)
	entry := registerForEvent(t, f.H, f.Ambassador, f.Event.ID, "massage-follower", "follower")
	code, raw := adminRequest(
		t,
		f.H,
		f.Admin,
		http.MethodPost,
		assignmentPath(f.Event.ID, entry.ID),
		assignmentBody("massage-follower-pass", f.Event, entry),
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	f.H.stop(t)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "UPDATE shuttle_events SET modules=array_append(modules,'massage'),version=version+1 WHERE id=$1 RETURNING version", f.Event.ID).
			Scan(&f.Event.Version),
	)
	doc := massageDocument(f.Event.Document.StartsAt)
	doc.Windows[0].Tables = tables
	specialist := doc.Specialists[0]
	specialist.ID = "beth"
	specialist.Name = massage.ScheduleLabel{EN: "Beth", RU: "Бет"}
	doc.Specialists = append(doc.Specialists, specialist)
	change := db.MassageScheduleChange{EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, Document: doc}
	admin := ownerProfileScope(t, f.H, f.AdminID)
	op := uuid.New()
	proposal, err := f.H.store.PrepareMassageSchedule(t.Context(), admin, op, change)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		admin,
		op,
		proposal.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	change.ExpectedVersion = 1
	return f, ownerProfileScope(t, f.H, f.OwnerID), ownerProfileScope(t, f.H, f.AmbassadorID), change
}

func massageBookingArgs(
	f paymentFixture,
	change db.MassageScheduleChange,
	slot int,
) db.MassageBookingArguments {
	return db.MassageBookingArguments{
		EventID:                 f.Event.ID,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: change.ExpectedVersion,
		Selection: massage.BookingSelection{
			WindowID:     "night",
			SpecialistID: "alex",
			ServiceID:    "short",
			StartsAt:     change.Document.Windows[0].StartsAt.Add(time.Duration(slot) * 20 * time.Minute),
		},
	}
}

func bookMassage(
	t *testing.T,
	f paymentFixture,
	scope db.ProfileScope,
	args db.MassageBookingArguments,
) (db.MassageBooking, db.MassageBookingPreparation) {
	t.Helper()
	op := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), scope, op, args)
	require.NoError(t, err)
	booking, err := f.H.store.BookOwnMassage(t.Context(), scope, op, proposal.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	return booking, proposal
}

func TestMassageBookingSharesProposalAcrossInterfacesAndPreservesOriginalReceipt(t *testing.T) {
	t.Parallel()
	f, owner, _, change := massageBookingFixture(t, 1)
	args := massageBookingArgs(f, change, 0)
	operation := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, operation, args)
	require.NoError(t, err)
	before, err := f.H.store.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, before.Bookings, "Preparation must not hold or create a reservation")
	first, err := f.H.store.BookOwnMassage(t.Context(), owner, operation, proposal.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	shared, err := f.H.store.BookOwnMassage(t.Context(), owner, uuid.New(), proposal.ID, args, db.MassageSourceMiniApp)
	require.NoError(t, err)
	require.Equal(t, first, shared)
	require.Equal(t, "booked", first.State)
	require.Equal(t, int64(4500), first.Snapshot.Terms.PriceMinor)
	require.Equal(t, 15*time.Minute, first.Snapshot.Terms.TreatmentEndsAt.Sub(first.Snapshot.Terms.StartsAt))
	require.Equal(t, 20*time.Minute, first.Snapshot.Terms.OccupancyEndsAt.Sub(first.Snapshot.Terms.StartsAt))
	reopened, err := db.Open(t.Context(), f.H.store.Pool().Config().ConnString())
	require.NoError(t, err)
	defer reopened.Close()
	prior, err := reopened.OwnMassageOperation(t.Context(), owner, operation)
	require.NoError(t, err)
	require.Equal(t, first, prior)
	cancelledArgs := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       first.ID,
		ExpectedVersion: first.Version,
	}
	cancelOp := uuid.New()
	cancellation, err := reopened.PrepareOwnMassageCancellation(t.Context(), owner, cancelOp, cancelledArgs)
	require.NoError(t, err)
	cancelled, err := reopened.CancelOwnMassage(t.Context(), owner, cancelOp, cancellation.ID, cancelledArgs)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.State)
	require.NotNil(t, cancelled.CancelledAt)
	replay, err := reopened.BookOwnMassage(t.Context(), owner, operation, proposal.ID, args, db.MassageSourceTelegram)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	current, err := reopened.OwnMassageBooking(t.Context(), owner, f.Event.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, cancelled, current)
	second, _ := bookMassage(t, f, owner, args)
	require.NotEqual(t, first.ID, second.ID, "Cancellation releases the occupied slot")
	var reviews int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_payment_reviews").Scan(&reviews),
	)
	require.Zero(t, reviews)
	_, err = f.H.store.OwnMassageBooking(t.Context(), owner, f.Event.ID, second.ID)
	require.NoError(t, err)
}

func TestMassageBookingSerializesSpecialistAndSharedTableContention(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"specialist", "table", "owner"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			assertMassageContention(t, kind)
		})
	}
}

func assertMassageContention(t *testing.T, kind string) {
	t.Helper()
	tables := 2
	if kind == "table" {
		tables = 1
	}
	f, owner, other, change := massageBookingFixture(t, tables)
	args := []db.MassageBookingArguments{
		massageBookingArgs(f, change, 0),
		massageBookingArgs(f, change, 0),
	}
	if kind != "specialist" {
		args[1].Selection.SpecialistID = "beth"
	}
	scopes := []db.ProfileScope{owner, other}
	if kind == "owner" {
		scopes[1] = owner
	}
	proposals := make([]db.MassageBookingPreparation, 2)
	operations := []uuid.UUID{uuid.New(), uuid.New()}
	for i := range scopes {
		var err error
		proposals[i], err = f.H.store.PrepareOwnMassageBooking(t.Context(), scopes[i], operations[i], args[i])
		require.NoError(t, err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	errors := make([]error, 2)
	for i := range scopes {
		wg.Go(func() {
			<-start
			_, errors[i] = f.H.store.BookOwnMassage(
				t.Context(),
				scopes[i],
				operations[i],
				proposals[i].ID,
				args[i],
				db.MassageSourceAgent,
			)
		})
	}
	close(start)
	wg.Wait()
	successes := 0
	for _, err := range errors {
		if err == nil {
			successes++
		} else {
			require.ErrorIs(t, err, massage.ErrUnavailable)
		}
	}
	require.Equal(t, 1, successes)
}

func TestMassageBookingDeniesChangedExpiredForeignAndIneligibleProposals(t *testing.T) {
	t.Parallel()
	f, owner, other, change := massageBookingFixture(t, 2)
	args := massageBookingArgs(f, change, 0)
	operation := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, operation, args)
	require.NoError(t, err)
	changed := args
	changed.Selection.SpecialistID = "beth"
	_, err = f.H.store.BookOwnMassage(t.Context(), owner, operation, proposal.ID, changed, db.MassageSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.BookOwnMassage(t.Context(), other, uuid.New(), proposal.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", proposal.ID)
	require.NoError(t, err)
	_, err = f.H.store.BookOwnMassage(t.Context(), owner, operation, proposal.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	_, err = f.H.store.PrepareOwnMassageBooking(t.Context(), admin, uuid.New(), args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestMassageBookingConfigGuardsAndOwnerCancellationSurvivePassLoss(t *testing.T) {
	t.Parallel()
	f, owner, _, change := massageBookingFixture(t, 2)
	args := massageBookingArgs(f, change, 0)
	first, _ := bookMassage(t, f, owner, args)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	changed := change
	changed.Document = massageDocument(f.Event.Document.StartsAt)
	changed.Document.Services[0].PriceMinor++
	op := uuid.New()
	proposal, err := f.H.store.PrepareMassageSchedule(t.Context(), admin, op, changed)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		admin,
		op,
		proposal.ID,
		changed,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	current, err := f.H.store.OwnMassageBooking(t.Context(), owner, f.Event.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, first, current)
	changed.ExpectedVersion = 2
	changed.Document.Services[0].Blocks = 2
	op = uuid.New()
	proposal, err = f.H.store.PrepareMassageSchedule(t.Context(), admin, op, changed)
	require.NoError(t, err)
	_, err = f.H.store.ChangeMassageSchedule(
		t.Context(),
		admin,
		op,
		proposal.ID,
		changed,
		func(context.Context) error { return nil },
	)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_registrations SET state='waitlisted' WHERE event_id=$1 AND user_id=$2", f.Event.ID, owner.UserID)
	require.NoError(t, err)
	cancellation := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       first.ID,
		ExpectedVersion: first.Version,
	}
	op = uuid.New()
	prepared, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, cancellation)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, prepared.ID, cancellation)
	require.NoError(t, err)
	_, err = f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	var count int
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_bookings WHERE state='booked'").
			Scan(&count),
	)
	require.Zero(t, count)
}

func TestMassageBookingHonorsRuntimePartyLimitAndOwnerPagination(t *testing.T) {
	t.Parallel()
	f, owner, _, change := massageBookingFixture(t, 2)
	for slot := range 3 {
		booking, proposal := bookMassage(t, f, owner, massageBookingArgs(f, change, slot))
		require.Equal(t, 3, proposal.Snapshot.MaxBookingsPerWindow)
		require.Equal(t, "booked", booking.State)
	}
	args := massageBookingArgs(f, change, 3)
	_, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, uuid.New(), args)
	require.ErrorIs(t, err, massage.ErrUnavailable)
	settings, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.MassageMaxBookingsPerWindow = 4
	_, err = f.H.store.CompareAndSwapSettings(
		t.Context(),
		settings.Version,
		settings.Document,
		ownerProfileScope(t, f.H, f.AdminID).Subject,
	)
	require.NoError(t, err)
	_, proposal := bookMassage(t, f, owner, args)
	require.Equal(t, 4, proposal.Snapshot.MaxBookingsPerWindow)
	page, err := f.H.store.OwnMassageBookings(t.Context(), owner, f.Event.ID, uuid.Nil, 2)
	require.NoError(t, err)
	require.Len(t, page.Bookings, 2)
	require.NotNil(t, page.NextAfter)
	rest, err := f.H.store.OwnMassageBookings(t.Context(), owner, f.Event.ID, *page.NextAfter, 2)
	require.NoError(t, err)
	require.Len(t, rest.Bookings, 2)
	require.Nil(t, rest.NextAfter)
}
