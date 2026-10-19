package integration_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/massage"
)

func massageAvailabilityArgs(
	f paymentFixture,
	change db.MassageScheduleChange,
	specialist string,
	slot, limit int,
) db.MassageAvailabilityArguments {
	start := change.Document.Windows[0].StartsAt.Add(time.Duration(slot) * 20 * time.Minute)
	return db.MassageAvailabilityArguments{
		EventID:                 f.Event.ID,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: change.ExpectedVersion,
		Query: massage.ScheduleCandidateQuery{
			WindowID:     "night",
			SpecialistID: specialist,
			ServiceID:    "short",
			From:         start,
			Until:        start.Add(time.Hour),
			Limit:        limit,
		},
	}
}

func cancelMassageForTest(t *testing.T, f paymentFixture, scope db.ProfileScope, booking db.MassageBooking) {
	t.Helper()
	op := uuid.New()
	args := db.MassageCancellationArguments{
		EventID:         booking.EventID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	prepared, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), scope, op, args)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), scope, op, prepared.ID, args)
	require.NoError(t, err)
}

func TestMassageAvailabilityObservesOccupancyWithoutHoldingCapacity(t *testing.T) {
	t.Parallel()
	f, owner, other, change := massageBookingFixture(t, 1)
	query := massageAvailabilityArgs(f, change, "beth", 0, 2)
	initial, err := f.H.store.OwnMassageAvailability(t.Context(), other, query)
	require.NoError(t, err)
	require.Len(t, initial.Candidates, 2)
	require.True(t, initial.Candidates[0].Available)
	require.False(t, initial.ScheduleOnly)
	require.True(t, initial.ReservationSupported)
	op := uuid.New()
	args := massageBookingArgs(f, change, 0)
	proposal, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, op, args)
	require.NoError(t, err)
	before, err := f.H.store.OwnMassageAvailability(t.Context(), other, query)
	require.NoError(t, err)
	require.True(t, before.Candidates[0].Available, "A preparation cannot hold capacity")
	booking, err := f.H.store.BookOwnMassage(t.Context(), owner, op, proposal.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	busy, err := f.H.store.OwnMassageAvailability(t.Context(), other, query)
	require.NoError(t, err)
	require.False(t, busy.Candidates[0].Available)
	require.Equal(t, "occupied", busy.Candidates[0].UnavailableReason)
	require.True(t, busy.Candidates[1].Available, "Half-open occupancy allows exact adjacency")
	require.NotNil(t, busy.NextAfter)
	query.Query.AfterStart = busy.NextAfter
	next, err := f.H.store.OwnMassageAvailability(t.Context(), other, query)
	require.NoError(t, err)
	require.Len(t, next.Candidates, 1)
	require.True(t, next.Candidates[0].Available)
	require.Nil(t, next.NextAfter)
	cancelMassageForTest(t, f, owner, booking)
	query.Query.AfterStart = nil
	available, err := f.H.store.OwnMassageAvailability(t.Context(), other, query)
	require.NoError(t, err)
	require.True(t, available.Candidates[0].Available)
	var proposals int
	require.NoError(
		t,
		f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_massage_preparations").Scan(&proposals),
	)
	require.Equal(t, 2, proposals, "Availability reads create no proposals")
}

func TestMassageAvailabilityAppliesPartyLimitAndCurrentAccess(t *testing.T) {
	t.Parallel()
	f, owner, _, change := massageBookingFixture(t, 2)
	var first db.MassageBooking
	for slot := range 3 {
		booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, change, slot))
		if slot == 0 {
			first = booking
		}
	}
	args := massageAvailabilityArgs(f, change, "alex", 3, 2)
	full, err := f.H.store.OwnMassageAvailability(t.Context(), owner, args)
	require.NoError(t, err)
	require.EqualValues(t, 3, full.CurrentBookings)
	require.Len(t, full.Candidates, 2)
	for _, slot := range full.Candidates {
		require.False(t, slot.Available)
		require.Equal(t, "party_limit", slot.UnavailableReason)
	}
	cancelMassageForTest(t, f, owner, first)
	free, err := f.H.store.OwnMassageAvailability(t.Context(), owner, args)
	require.NoError(t, err)
	require.EqualValues(t, 2, free.CurrentBookings)
	require.True(t, free.Candidates[0].Available)
	stale := args
	stale.ExpectedScheduleVersion++
	_, err = f.H.store.OwnMassageAvailability(t.Context(), owner, stale)
	require.ErrorIs(t, err, db.ErrConflict)
	_, err = f.H.store.OwnMassageAvailability(t.Context(), ownerProfileScope(t, f.H, f.AdminID), args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage'),version=version+1 WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	_, err = f.H.store.OwnMassageAvailability(t.Context(), owner, args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestMassageAvailabilityMaximumPageFitsAdvertisedResult(t *testing.T) {
	t.Parallel()
	f, owner, _, change := massageBookingFixture(t, 2)
	change.Document.BlockSeconds = 60
	change.Document.BufferSeconds = 10
	admin := ownerProfileScope(t, f.H, f.AdminID)
	op := uuid.New()
	proposal, err := f.H.store.PrepareMassageSchedule(t.Context(), admin, op, change)
	require.NoError(t, err)
	changed, err := f.H.store.ChangeMassageSchedule(
		t.Context(),
		admin,
		op,
		proposal.ID,
		change,
		func(context.Context) error { return nil },
	)
	require.NoError(t, err)
	change.ExpectedVersion = changed.Version
	args := massageAvailabilityArgs(f, change, "alex", 0, 100)
	args.Query.Until = args.Query.From.Add(4 * time.Hour)
	page, err := f.H.store.OwnMassageAvailability(t.Context(), owner, args)
	require.NoError(t, err)
	require.Len(t, page.Candidates, 100)
	require.True(t, page.HasMore)
	raw, err := json.Marshal(page)
	require.NoError(t, err)
	require.Less(t, len(raw), 60<<10)
	for _, slot := range page.Candidates {
		require.True(t, slot.Available)
	}
}
