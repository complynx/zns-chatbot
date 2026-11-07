package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMassageBookingPreventsTableReductionUntilCapacityIsReleased(t *testing.T) {
	t.Parallel()
	f, owner, other, change := massageBookingFixture(t, 2)
	args := massageBookingArgs(f, change, 0)
	first, _ := bookMassage(t, f, owner, args)
	args.Selection.SpecialistID = "beth"
	second, _ := bookMassage(t, f, other, args)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	change.Document.Windows[0].Tables = 1
	operation := uuid.New()
	prepared, err := f.H.store.PrepareMassageSchedule(t.Context(), admin, operation, change)
	require.NoError(t, err)
	check := func(context.Context) error { return nil }
	_, err = f.H.store.ChangeMassageSchedule(t.Context(), admin, operation, prepared.ID, change, check)
	require.ErrorIs(t, err, db.ErrConflict)
	current, err := f.H.store.MassageScheduleConfiguration(t.Context(), admin, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, int64(1), current.Version)
	require.Equal(t, 2, current.Document.Windows[0].Tables)
	cancellation := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       second.ID,
		ExpectedVersion: second.Version,
	}
	cancelOp := uuid.New()
	cancelProposal, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), other, cancelOp, cancellation)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), other, cancelOp, cancelProposal.ID, cancellation)
	require.NoError(t, err)
	// The exact proposal can now succeed because the competing commitment was cancelled.
	changed, err := f.H.store.ChangeMassageSchedule(t.Context(), admin, operation, prepared.ID, change, check)
	require.NoError(t, err)
	require.Equal(t, 1, changed.Document.Windows[0].Tables)
	retained, err := f.H.store.OwnMassageBooking(t.Context(), owner, f.Event.ID, first.ID)
	require.NoError(t, err)
	require.Equal(t, first, retained)
}

func TestMassageBookingPreventsEventBoundsFromExcludingAnActiveReservation(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	before, err := f.H.store.Event(t.Context(), scope, f.Event.ID)
	require.NoError(t, err)
	change := db.EventChange{ID: f.Event.ID, ExpectedVersion: before.Version, Document: before.Document}
	change.Document.EndsAt = booking.Snapshot.Terms.OccupancyEndsAt.Add(-time.Microsecond)
	change.Document.Registration, change.Document.Deposit = nil, nil
	admin := ownerProfileScope(t, f.H, f.AdminID)
	_, err = f.H.store.ChangeEvent(t.Context(), scope, admin.Subject, uuid.NewString(), change)
	require.ErrorIs(t, err, db.ErrConflict)
	after, err := f.H.store.Event(t.Context(), scope, f.Event.ID)
	require.NoError(t, err)
	require.Equal(t, before, after)
	operation := uuid.New()
	args := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	prepared, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, operation, args)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), owner, operation, prepared.ID, args)
	require.NoError(t, err)
	changed, err := f.H.store.ChangeEvent(t.Context(), scope, admin.Subject, uuid.NewString(), change)
	require.NoError(t, err)
	require.True(t, changed.Document.EndsAt.Equal(change.Document.EndsAt))
}
