package integration_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func bookingReminders(t *testing.T, f paymentFixture, booking uuid.UUID) map[string]db.MassageReminder {
	t.Helper()
	rows, err := f.H.store.Pool().
		Query(t.Context(), "SELECT id FROM shuttle_massage_reminders WHERE booking_id=$1 ORDER BY kind", booking)
	require.NoError(t, err)
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		require.NoError(t, rows.Scan(&id))
		ids = append(ids, id)
	}
	require.NoError(t, rows.Err())
	rows.Close()
	result := map[string]db.MassageReminder{}
	for _, id := range ids {
		owner := ownerProfileScope(t, f.H, f.OwnerID)
		scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
		n, e := f.H.store.MassageReminder(t.Context(), scope, id)
		require.NoError(t, e)
		require.NotContains(t, result, n.Kind)
		result[n.Kind] = n
	}
	return result
}

func TestMassageRemindersShareBookingReplayAndCancellationAcrossInterfaces(t *testing.T) {
	t.Parallel()
	for _, native := range []bool{false, true} {
		name := "agent"
		if native {
			name = "native"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, owner, _, schedule := massageBookingFixture(t, 1)
			deployment := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
			session := nativeMassageSession(t, f, owner, 11002)
			args := massageBookingArgs(f, schedule, 0)
			op := uuid.New()
			var proposal db.MassageBookingPreparation
			var booking db.MassageBooking
			var err error
			if native {
				proposal, err = f.H.store.PrepareMiniAppMassageBooking(t.Context(), deployment, session, op, args)
				require.NoError(t, err)
				booking, err = f.H.store.BookMiniAppMassage(t.Context(), deployment, session, op, proposal.ID, args)
			} else {
				proposal, err = f.H.store.PrepareOwnMassageBooking(t.Context(), owner, op, args)
				require.NoError(t, err)
				booking, err = f.H.store.BookOwnMassage(
					t.Context(),
					owner,
					op,
					proposal.ID,
					args,
					db.MassageSourceAgent,
				)
			}
			require.NoError(t, err)
			notices := bookingReminders(t, f, booking.ID)
			require.Len(t, notices, 2)
			for kind, lead := range map[string]time.Duration{
				db.MassageReminderLong:  time.Hour,
				db.MassageReminderShort: 10 * time.Minute,
			} {
				n := notices[kind]
				require.Equal(t, owner.UserID, n.RecipientID)
				require.Equal(t, booking, n.Booking)
				require.Equal(t, int32(lead/time.Second), n.LeadSeconds)
				require.Equal(t, booking.Snapshot.Terms.StartsAt.Add(-lead).UTC(), n.DueAt)
				require.WithinDuration(t, booking.BookedAt, n.NextAttemptAt, time.Second)
				require.Equal(t, "pending", n.State)
				require.Equal(t, int64(1), n.Version)
			}
			replay, err := f.H.store.BookOwnMassage(t.Context(), owner, op, proposal.ID, args, db.MassageSourceAgent)
			require.NoError(t, err)
			require.Equal(t, booking, replay)
			replay, err = f.H.store.BookMiniAppMassage(t.Context(), deployment, session, uuid.New(), proposal.ID, args)
			require.NoError(t, err)
			require.Equal(t, booking, replay)
			require.Equal(
				t,
				notices,
				bookingReminders(t, f, booking.ID),
				"Replays must retain the two original intents",
			)
			for _, scope := range []db.EventScope{
				{BotID: owner.BotID + 1, Organization: owner.Organization},
				{BotID: owner.BotID, Organization: "foreign-org"},
			} {
				_, err = f.H.store.MassageReminder(t.Context(), scope, notices[db.MassageReminderLong].ID)
				require.ErrorIs(t, err, pgx.ErrNoRows)
			}
			cancellation := db.MassageCancellationArguments{
				EventID:         f.Event.ID,
				BookingID:       booking.ID,
				ExpectedVersion: booking.Version,
			}
			cancelOp := uuid.New()
			prepared, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, cancelOp, cancellation)
			require.NoError(t, err)
			if native {
				_, err = f.H.store.CancelMiniAppMassage(
					t.Context(),
					deployment,
					session,
					cancelOp,
					prepared.ID,
					cancellation,
				)
			} else {
				_, err = f.H.store.CancelOwnMassage(t.Context(), owner, cancelOp, prepared.ID, cancellation)
			}
			require.NoError(t, err)
			for kind, n := range bookingReminders(t, f, booking.ID) {
				require.Equal(t, "delivery_blocked", n.State)
				require.Equal(t, int64(2), n.Version)
				require.Equal(t, notices[kind].ID, n.ID)
				require.Equal(t, notices[kind].DueAt, n.DueAt)
				require.Equal(t, "cancelled", n.Booking.State)
			}
			require.Len(t, bookingReminders(t, f, booking.ID), 2)
		})
	}
}

func TestMassageReminderCancellationReceiptFailureRollsBackSuppression(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 1)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	before := bookingReminders(t, f, booking.ID)
	args := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       booking.ID,
		ExpectedVersion: booking.Version,
	}
	op := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, args)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_reminder_cancel_receipt() RETURNS trigger LANGUAGE plpgsql "+
			"AS $$ BEGIN RAISE EXCEPTION 'synthetic reminder cancel receipt failure'; END $$;\n"+
			" CREATE TRIGGER fail_reminder_cancel_receipt BEFORE INSERT ON "+
			"shuttle_massage_operations FOR EACH ROW EXECUTE FUNCTION "+
			"fail_reminder_cancel_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, proposal.ID, args)
	require.ErrorContains(t, err, "synthetic reminder cancel receipt failure")
	require.Equal(t, before, bookingReminders(t, f, booking.ID))
	_, err = f.H.store.Pool().
		Exec(t.Context(), "DROP TRIGGER fail_reminder_cancel_receipt ON shuttle_massage_operations; DROP "+
			"FUNCTION fail_reminder_cancel_receipt()")
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, proposal.ID, args)
	require.NoError(t, err)
	for _, n := range bookingReminders(t, f, booking.ID) {
		require.Equal(t, "delivery_blocked", n.State)
	}
}

func TestMassageReminderSettingsRescheduleOnlySchedulableAndKeepRetryEmbargo(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 2)
	bookings := []db.MassageBooking{}
	for slot := range 3 {
		b, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, slot))
		bookings = append(bookings, b)
	}
	first := bookingReminders(t, f, bookings[0].ID)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_reminders SET state='retryable',"+
			"next_attempt_at=clock_timestamp()+interval '1 day' WHERE id=$1", first[db.MassageReminderShort].ID)
	require.NoError(t, err)
	for index, state := range []string{"delivered", "delivery_unknown", "send_inflight", "delivery_blocked"} {
		b := bookings[1+index/2]
		kind := db.MassageReminderLong
		if index%2 == 1 {
			kind = db.MassageReminderShort
		}
		_, err = f.H.store.Pool().
			Exec(t.Context(), "UPDATE shuttle_massage_reminders SET state=$2,attempt_id=$3 WHERE booking_id=$1 "+
				"AND kind=$4", b.ID, state, uuid.New(), kind)
		require.NoError(t, err)
	}
	before := []map[string]db.MassageReminder{}
	for _, b := range bookings {
		before = append(before, bookingReminders(t, f, b.ID))
	}
	settings, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.MassageNotifyClientPriorLongSeconds = 7200
	settings.Document.MassageNotifyClientPriorSeconds = 300
	changed, err := f.H.store.CompareAndSwapSettings(
		t.Context(),
		settings.Version,
		settings.Document,
		"reminder-settings-admin",
	)
	require.NoError(t, err)
	for index, b := range bookings {
		for kind, n := range bookingReminders(t, f, b.ID) {
			old := before[index][kind]
			if index != 0 {
				require.Equal(t, old, n)
				continue
			}
			seconds := int32(7200)
			if kind == db.MassageReminderShort {
				seconds = 300
			}
			require.Equal(t, seconds, n.LeadSeconds)
			require.Equal(t, b.Snapshot.Terms.StartsAt.Add(-time.Duration(seconds)*time.Second).UTC(), n.DueAt)
			require.Equal(t, old.Version+1, n.Version)
			require.Equal(t, old.NextAttemptAt, n.NextAttemptAt)
			require.Equal(t, old.State, n.State)
		}
	}
	first = bookingReminders(t, f, bookings[0].ID)
	changed.Document.DefaultLanguage = "ru"
	_, err = f.H.store.CompareAndSwapSettings(t.Context(), changed.Version, changed.Document, "reminder-settings-admin")
	require.NoError(t, err)
	require.Equal(t, first, bookingReminders(t, f, bookings[0].ID), "Unrelated settings do not revise reminder intents")
}

func TestMassageReminderSettingsFailureRollsBackSettingsAuditAndDueTimes(t *testing.T) {
	t.Parallel()
	f, owner, _, schedule := massageBookingFixture(t, 1)
	b, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	notices := bookingReminders(t, f, b.ID)
	settings, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	audits := settingsAuditCount(t, f.H)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_reminder_reschedule() RETURNS trigger LANGUAGE plpgsql AS $$ "+
			"BEGIN RAISE EXCEPTION 'synthetic reminder reschedule failure'; END $$;\n"+
			" CREATE TRIGGER fail_reminder_reschedule BEFORE UPDATE ON "+
			"shuttle_massage_reminders FOR EACH ROW EXECUTE FUNCTION fail_reminder_reschedule()")
	require.NoError(t, err)
	doc := settings.Document
	doc.MassageNotifyClientPriorLongSeconds = 7200
	_, err = f.H.store.CompareAndSwapSettings(t.Context(), settings.Version, doc, "reminder-settings-admin")
	require.ErrorContains(t, err, "synthetic reminder reschedule failure")
	current, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, settings, current)
	require.Equal(t, audits, settingsAuditCount(t, f.H))
	require.Equal(t, notices, bookingReminders(t, f, b.ID))
}

func TestMassageReminderSettingsSerializeWithBookingAndCancellation(t *testing.T) {
	t.Parallel()
	for _, cancelBooking := range []bool{false, true} {
		name := "booking"
		if cancelBooking {
			name = "cancellation"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, owner, _, schedule := massageBookingFixture(t, 1)
			args := massageBookingArgs(f, schedule, 0)
			op := uuid.New()
			prep, err := f.H.store.PrepareOwnMassageBooking(t.Context(), owner, op, args)
			require.NoError(t, err)
			var booking db.MassageBooking
			var cancellation db.MassageCancellationArguments
			var cancelPrep db.MassageCancellationPreparation
			if cancelBooking {
				booking, err = f.H.store.BookOwnMassage(t.Context(), owner, op, prep.ID, args, db.MassageSourceAgent)
				require.NoError(t, err)
				cancellation = db.MassageCancellationArguments{
					EventID:         f.Event.ID,
					BookingID:       booking.ID,
					ExpectedVersion: booking.Version,
				}
				cancelPrep, err = f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, uuid.New(), cancellation)
				require.NoError(t, err)
			}
			settings, err := f.H.store.Settings(t.Context())
			require.NoError(t, err)
			settings.Document.MassageNotifyClientPriorLongSeconds = 7200
			ctx, stop := context.WithTimeout(t.Context(), 10*time.Second)
			defer stop()
			start := make(chan struct{})
			errors := make([]error, 2)
			var group sync.WaitGroup
			group.Go(func() {
				<-start
				_, errors[0] = f.H.store.CompareAndSwapSettings(
					ctx,
					settings.Version,
					settings.Document,
					"reminder-settings-admin",
				)
			})
			group.Go(func() {
				<-start
				if cancelBooking {
					booking, errors[1] = f.H.store.CancelOwnMassage(ctx, owner, uuid.New(), cancelPrep.ID, cancellation)
				} else {
					booking, errors[1] = f.H.store.BookOwnMassage(ctx, owner, op, prep.ID, args, db.MassageSourceAgent)
				}
			})
			close(start)
			group.Wait()
			require.NoError(t, errors[0])
			require.NoError(t, errors[1])
			notices := bookingReminders(t, f, booking.ID)
			require.Len(t, notices, 2)
			if cancelBooking {
				for _, n := range notices {
					require.Equal(t, "delivery_blocked", n.State)
				}
			} else {
				require.Equal(t, int32(7200), notices[db.MassageReminderLong].LeadSeconds)
				require.Equal(
					t,
					booking.Snapshot.Terms.StartsAt.Add(-2*time.Hour).UTC(),
					notices[db.MassageReminderLong].DueAt,
				)
			}
		})
	}
}
