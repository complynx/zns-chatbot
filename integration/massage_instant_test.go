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

// The specialist's current party is near the database clock, not the ordinary
// registration fixture's future event. No application clock is replaced.
func instantFixture(
	t *testing.T,
) (paymentFixture, db.ProfileScope, db.MassageInstantArguments, db.MassageBindingChange) {
	t.Helper()
	f, staff, schedule, binding := staffFixture(t)
	args := moveMassagePartyToNow(t, &f, staff, schedule)
	binding.ExpectedEventVersion, binding.ExpectedScheduleVersion = args.ExpectedEventVersion, args.ExpectedScheduleVersion
	return f, staff, args, binding
}

func moveMassagePartyToNow(t *testing.T, f *paymentFixture, admin db.ProfileScope,
	schedule db.MassageScheduleChange,
) db.MassageInstantArguments {
	t.Helper()
	var now time.Time
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&now))
	start := now.UTC().Truncate(time.Minute).Add(-2 * time.Minute)
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"UPDATE shuttle_events SET starts_at=$2,ends_at=$3,version=version+1 WHERE id=$1 RETURNING version",
		f.Event.ID, start.Add(-time.Hour), start.Add(5*time.Hour)).Scan(&f.Event.Version))
	schedule.ExpectedEventVersion = f.Event.Version
	shift := start.Sub(schedule.Document.Windows[0].StartsAt)
	for i := range schedule.Document.Windows {
		w := &schedule.Document.Windows[i]
		w.StartsAt, w.EndsAt = w.StartsAt.Add(shift), w.EndsAt.Add(shift)
	}
	for i := range schedule.Document.Specialists {
		spans := append([]massage.ScheduleWorkSpan(nil), schedule.Document.Specialists[i].WorkSpans...)
		for j := range spans {
			span := &spans[j]
			span.StartsAt, span.EndsAt = span.StartsAt.Add(shift), span.EndsAt.Add(shift)
		}
		schedule.Document.Specialists[i].WorkSpans = spans
	}
	schedule.Document.Windows[0].Tables = 1
	op := uuid.New()
	prep, err := f.H.store.PrepareMassageSchedule(t.Context(), admin, op, schedule)
	require.NoError(t, err)
	changed, err := f.H.store.ChangeMassageSchedule(t.Context(), admin, op, prep.ID, schedule,
		func(context.Context) error { return nil })
	require.NoError(t, err)
	return db.MassageInstantArguments{
		EventID: f.Event.ID, ExpectedEventVersion: f.Event.Version, ExpectedScheduleVersion: changed.Version,
		Selection: massage.InstantSelection{WindowID: "night", SpecialistID: "alex", ServiceID: "short"},
	}
}

func assertMassageWireEqual(t *testing.T, want, got any) {
	t.Helper()
	expected, err := json.Marshal(want)
	require.NoError(t, err)
	actual, err := json.Marshal(got)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(actual))
}

func TestMassageInstantSharesFrozenProposalAndReceiptAcrossInterfaces(t *testing.T) {
	t.Parallel()
	f, staff, args, binding := instantFixture(t)
	deployment := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
	session := nativeMassageSession(t, f, staff, 11001)
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, op, args)
	require.NoError(t, err)
	retry, err := f.H.store.PrepareMiniAppMassageInstant(t.Context(), deployment, session, op, args)
	require.NoError(t, err)
	assertMassageWireEqual(t, prep, retry)
	require.Equal(t, "staff_self", prep.Snapshot.Purpose)
	require.Equal(t, int64(1), prep.Snapshot.StaffBindingVersion)
	before, err := f.H.store.OwnMassageBookings(t.Context(), staff, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, before.Bookings, "Preparation must not hold capacity")
	pending, err := f.H.store.OwnPendingMassageProposals(t.Context(), staff, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, pending.Proposals, 1)
	require.Equal(t, &args, pending.Proposals[0].Instant)
	require.Nil(t, pending.Proposals[0].Booking)
	adopted, err := f.H.store.AdoptMiniAppMassageInstant(t.Context(), deployment, session, uuid.New(), prep.ID, args)
	require.NoError(t, err)
	assertMassageWireEqual(t, prep, adopted)
	first, err := f.H.store.BookMiniAppMassageInstant(t.Context(), deployment, session, op, prep.ID, args)
	require.NoError(t, err)
	assertMassageWireEqual(t, prep.Snapshot, first.Snapshot)
	second, err := f.H.store.BookOwnMassageInstant(t.Context(), staff, uuid.New(), prep.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	require.Equal(t, first, second)
	require.Empty(t, specialistNotices(t, f, first.ID), "Self booking suppresses only the initial booked notice")
	var reminders int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_massage_reminders WHERE booking_id=$1", first.ID).Scan(&reminders))
	require.Positive(t, reminders)
	binding.Active = false
	bindingChange(t, f, staff, binding)
	// A committed receipt and owner cancellation survive loss of the staff grant.
	replay, err := f.H.store.BookOwnMassageInstant(t.Context(), staff, op, prep.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	require.Equal(t, first, replay)
	cancel := db.MassageCancellationArguments{EventID: f.Event.ID, BookingID: first.ID, ExpectedVersion: first.Version}
	cancelOp := uuid.New()
	cp, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), staff, cancelOp, cancel)
	require.NoError(t, err)
	result, err := f.H.store.CancelOwnMassage(t.Context(), staff, cancelOp, cp.ID, cancel)
	require.NoError(t, err)
	require.Equal(t, "cancelled", result.State)
	require.Empty(t, specialistNotices(t, f, first.ID), "Revocation must not notify an inactive alias")
}

func TestMassageInstantRequiresCurrentOwnAliasAndRetainsFrozenConsent(t *testing.T) {
	t.Parallel()
	f, staff, args, binding := instantFixture(t)
	other := ownerProfileScope(t, f.H, f.OwnerID)
	_, err := f.H.store.PrepareOwnMassageInstant(t.Context(), other, uuid.New(), args)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	foreign := args
	foreign.Selection.SpecialistID = "beth"
	_, err = f.H.store.PrepareOwnMassageInstant(t.Context(), staff, uuid.New(), foreign)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, op, args)
	require.NoError(t, err)
	_, err = f.H.store.BookOwnMassageInstant(t.Context(), other, uuid.New(), prep.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	ordinary := db.MassageBookingArguments{
		EventID: args.EventID, ExpectedEventVersion: args.ExpectedEventVersion,
		ExpectedScheduleVersion: args.ExpectedScheduleVersion, Selection: prep.Snapshot.Terms.BookingSelection,
	}
	_, err = f.H.store.BookOwnMassage(t.Context(), staff, uuid.New(), prep.ID, ordinary, db.MassageSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict, "Ordinary route cannot consume staff consent")
	binding.Active = false
	revoked := bindingChange(t, f, staff, binding)
	_, err = f.H.store.BookOwnMassageInstant(t.Context(), staff, op, prep.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	binding.Active, binding.ExpectedVersion = true, revoked.Version
	bindingChange(t, f, staff, binding)
	_, err = f.H.store.AdoptOwnMassageInstant(t.Context(), staff, uuid.New(), prep.ID, args)
	require.ErrorIs(t, err, db.ErrConflict, "Regrant cannot revive consent from an older binding")
	_, err = f.H.store.BookOwnMassageInstant(t.Context(), staff, op, prep.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	retained, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, op, args)
	require.NoError(t, err)
	assertMassageWireEqual(t, prep, retained)
	list, err := f.H.store.OwnMassageBookings(t.Context(), staff, f.Event.ID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Empty(t, list.Bookings)
}

func TestMassageInstantRechecksCapacityExpiryAndEventVersion(t *testing.T) {
	t.Parallel()
	f, staff, args, binding := instantFixture(t)
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, op, args)
	require.NoError(t, err)
	competing, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, uuid.New(), args)
	require.NoError(t, err)
	booked, err := f.H.store.BookOwnMassageInstant(t.Context(), staff, op, prep.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	_, err = f.H.store.BookOwnMassageInstant(t.Context(), staff, uuid.New(), competing.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, massage.ErrUnavailable, "Preparation holds no capacity; apply must recheck")
	other := ownerProfileScope(t, f.H, f.OwnerID)
	binding.SpecialistID, binding.UserID, binding.ExpectedVersion = "beth", other.UserID, 0
	bindingChange(t, f, staff, binding)
	otherArgs := args
	otherArgs.Selection.SpecialistID = "beth"
	_, err = f.H.store.PrepareOwnMassageInstant(t.Context(), other, uuid.New(), otherArgs)
	require.ErrorIs(t, err, massage.ErrUnavailable, "Another specialist still requires the occupied table")
	cancel := db.MassageCancellationArguments{
		EventID:         args.EventID,
		BookingID:       booked.ID,
		ExpectedVersion: booked.Version,
	}
	co := uuid.New()
	cp, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), staff, co, cancel)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), staff, co, cp.ID, cancel)
	require.NoError(t, err)
	require.Len(t, specialistNotices(t, f, booked.ID), 1)
	require.Equal(t, "cancelled", specialistNotices(t, f, booked.ID)[0].Kind)
	_, err = f.H.store.Pool().Exec(t.Context(),
		"UPDATE shuttle_massage_preparations SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", competing.ID)
	require.NoError(t, err)
	_, err = f.H.store.BookOwnMassageInstant(t.Context(), staff, uuid.New(), competing.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	fresh, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, uuid.New(), args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().Exec(t.Context(), "UPDATE shuttle_events SET version=version+1 WHERE id=$1", args.EventID)
	require.NoError(t, err)
	_, err = f.H.store.BookOwnMassageInstant(t.Context(), staff, uuid.New(), fresh.ID, args, db.MassageSourceAgent)
	require.ErrorIs(t, err, db.ErrConflict)
	list, err := f.H.store.OwnMassageBookings(t.Context(), staff, args.EventID, uuid.Nil, 20)
	require.NoError(t, err)
	require.Len(t, list.Bookings, 1)
	require.Equal(t, "cancelled", list.Bookings[0].State)
}
