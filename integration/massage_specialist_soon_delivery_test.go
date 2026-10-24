package integration_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func soonSelfBooking(t *testing.T) (paymentFixture, db.ProfileScope, db.MassageBooking, db.MassageBindingChange) {
	t.Helper()
	f, staff, args, binding := instantFixture(t)
	op := uuid.New()
	prep, err := f.H.store.PrepareOwnMassageInstant(t.Context(), staff, op, args)
	require.NoError(t, err)
	booking, err := f.H.store.BookOwnMassageInstant(t.Context(), staff, op, prep.ID, args, db.MassageSourceAgent)
	require.NoError(t, err)
	return f, staff, booking, binding
}

func soonNotice(t *testing.T, f paymentFixture, booking uuid.UUID) specialistNoticeRow {
	t.Helper()
	rows := specialistNotices(t, f, booking)
	for _, row := range rows {
		if row.Kind == "soon" {
			return row
		}
	}
	t.Fatal("Upcoming-session notice is missing")
	return specialistNoticeRow{}
}

func setSoonPreference(t *testing.T, f paymentFixture, staff db.ProfileScope, version int64, enabled bool) {
	t.Helper()
	_, err := f.H.store.SetMassageSpecialistPreferences(
		t.Context(),
		staff,
		uuid.New(),
		db.MassageSpecialistPreferenceChange{
			EventID: f.Event.ID, ExpectedVersion: version, NotifyNext: &enabled,
		},
	)
	require.NoError(t, err)
}

func TestMassageSpecialistSoonDeliveryCapturesOnlyDueScopedActiveEvents(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	scope := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
	require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope))
	require.Len(t, specialistNotices(t, f, booking.ID), 1, "Future session has only its initial booked notice")
	// Move the fixture session, preserving its frozen terms, to exercise late catch-up without sleeps.
	var now time.Time
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT clock_timestamp()").Scan(&now))
	terms := &booking.Snapshot.Terms
	shift := now.Add(-time.Minute).Sub(terms.StartsAt)
	terms.StartsAt = terms.StartsAt.Add(shift)
	terms.TreatmentEndsAt = terms.TreatmentEndsAt.Add(shift)
	terms.OccupancyEndsAt = terms.OccupancyEndsAt.Add(shift)
	raw, err := json.Marshal(booking.Snapshot)
	require.NoError(t, err)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_massage_bookings SET starts_at=$2,treatment_ends_at=$3,oc" +
"cupancy_ends_at=$4,snapshot=$5 WHERE id=$1", booking.ID, terms.StartsAt, terms.TreatmentEndsAt, terms.OccupancyEndsAt, raw)
	require.NoError(t, err)
	require.NoError(
		t,
		f.H.store.CreateDueMassageSpecialistNotifications(
			t.Context(),
			db.EventScope{BotID: scope.BotID + 1, Organization: scope.Organization},
		),
	)
	require.Len(t, specialistNotices(t, f, booking.ID), 1)
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET starts_at=$2,ends_at=$3 WHERE id=$1", f.Event.ID, now.Add(-time.Hour), now.Add(-time.Minute))
	require.NoError(t, err)
	require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope))
	require.Len(t, specialistNotices(t, f, booking.ID), 1, "Ended events never backfill reminders")
	_, err = f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET ends_at=$2 WHERE id=$1", f.Event.ID, now.Add(time.Hour))
	require.NoError(t, err)
	require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope))
	require.Equal(t, "pending", soonNotice(t, f, booking.ID).State)
}

func TestMassageSpecialistSoonDeliveryHonorsPreferenceAtDueAndDoesNotRevive(t *testing.T) {
	t.Parallel()
	for _, reenableBeforeDue := range []bool{false, true} {
		t.Run(
			map[bool]string{false: "muted_at_due", true: "enabled_before_due"}[reenableBeforeDue],
			func(t *testing.T) {
				t.Parallel()
				f, staff, booking, _ := soonSelfBooking(t)
				scope := db.EventScope{BotID: staff.BotID, Organization: staff.Organization}
				setSoonPreference(t, f, staff, 0, false)
				if reenableBeforeDue {
					setSoonPreference(t, f, staff, 1, true)
				}
				require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope))
				first := soonNotice(t, f, booking.ID)
				if reenableBeforeDue {
					require.Equal(t, "pending", first.State)
				} else {
					require.Equal(t, "delivery_blocked", first.State)
					setSoonPreference(t, f, staff, 1, true)
				}
				require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope))
				require.Equal(
					t,
					first,
					soonNotice(t, f, booking.ID),
					"Restart or re-enable cannot recreate a consumed session notice",
				)
				require.Len(
					t,
					specialistNotices(t, f, booking.ID),
					1,
					"Self bookings suppress initial booked notices, not upcoming ones",
				)
			},
		)
	}
}

func TestMassageSpecialistSoonDeliveryRechecksCapturedAuthorityAndCancellation(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"next_reenable", "bookings_reenable", "regrant", "revoked_user", "cancelled", "ended"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, staff, booking, binding := soonSelfBooking(t)
			scope := paymentScope(t, f.H, staff.UserID, false)
			require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope.EventScope))
			notice := soonNotice(t, f, booking.ID)
			_, err := f.H.store.MassageSpecialistNotificationRecipient(t.Context(), scope.EventScope, notice.ID)
			require.NoError(t, err)
			switch change {
			case "next_reenable":
				setSoonPreference(t, f, staff, 0, false)
				setSoonPreference(t, f, staff, 1, true)
			case "bookings_reenable":
				for i, v := range []bool{false, true} {
					_, err = f.H.store.SetMassageSpecialistPreferences(
						t.Context(),
						staff,
						uuid.New(),
						noticePreference(f.Event.ID, int64(i), v),
					)
					require.NoError(t, err)
				}
			case "regrant":
				binding.Active = false
				revoked := bindingChange(t, f, staff, binding)
				binding.Active = true
				binding.ExpectedVersion = revoked.Version
				bindingChange(t, f, staff, binding)
			case "revoked_user":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
				require.NoError(t, err)
			case "cancelled":
				cancelMassageForTest(t, f, staff, booking)
			case "ended":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET ends_at=clock_timestamp()-interval '1 second' " +
"WHERE id=$1", f.Event.ID)
				require.NoError(t, err)
			}
			reserved, err := f.H.store.ReserveMassageSpecialistNotification(
				t.Context(),
				scope,
				notice.ID,
				notice.Version,
				json.RawMessage(`{"text":"soon"}`),
			)
			if change == "bookings_reenable" {
				require.NoError(t, err)
				require.NotEqual(t, uuid.Nil, reserved.AttemptID)
				return
			}
			require.Error(t, err)
			current, err := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, notice.ID)
			require.NoError(t, err)
			require.Equal(t, uuid.Nil, current.AttemptID)
			require.Empty(t, current.Payload)
			if change == "cancelled" {
				require.Equal(t, "delivery_blocked", current.State)
			}
		})
	}
}

func TestMassageSpecialistSoonDeliveryRecoveryKeepsOriginalUnknownAttempt(t *testing.T) {
	t.Parallel()
	f, staff, booking, _ := soonSelfBooking(t)
	scope := paymentScope(t, f.H, staff.UserID, false)
	require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope.EventScope))
	n := soonNotice(t, f, booking.ID)
	reserved, err := f.H.store.ReserveMassageSpecialistNotification(
		t.Context(),
		scope,
		n.ID,
		n.Version,
		json.RawMessage(`{"text":"possibly sent"}`),
	)
	require.NoError(t, err)
	require.NoError(t, f.H.store.RecoverMassageSpecialistNotifications(t.Context(), scope.EventScope))
	require.NoError(t, f.H.store.CreateDueMassageSpecialistNotifications(t.Context(), scope.EventScope))
	saved, err := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, n.ID)
	require.NoError(t, err)
	require.Equal(t, "delivery_unknown", saved.State)
	require.Equal(t, reserved.AttemptID, saved.AttemptID)
	require.JSONEq(t, string(reserved.Payload), string(saved.Payload))
	ready, err := f.H.store.ReadyMassageSpecialistNotifications(t.Context(), scope.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, ready)
	_, err = f.H.store.ReserveMassageSpecialistNotification(
		t.Context(),
		scope,
		n.ID,
		saved.Version,
		json.RawMessage(`{}`),
	)
	require.ErrorIs(t, err, db.ErrConflict)
}

func TestMassageSpecialistSoonDeliveryRetainsHistory(t *testing.T) {
	t.Parallel()
	f, staff, booking, _ := soonSelfBooking(t)
	require.NoError(
		t,
		f.H.store.CreateDueMassageSpecialistNotifications(
			t.Context(),
			db.EventScope{BotID: staff.BotID, Organization: staff.Organization},
		),
	)
	before := soonNotice(t, f, booking.ID)
	require.Equal(t, before, soonNotice(t, f, booking.ID))
	// The queued notification remains readable through the store.
	_, err := f.H.store.MassageSpecialistNotification(
		t.Context(),
		db.EventScope{BotID: staff.BotID, Organization: staff.Organization},
		before.ID,
	)
	require.NotErrorIs(t, err, pgx.ErrNoRows)
	require.NoError(t, err)
}

func TestMassageSpecialistSoonWorkerDeliversSelfReservationWithoutLoom(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "uk"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, staff, booking, _ := soonSelfBooking(t)
			_, err := f.H.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_users SET profile=$2 WHERE id=$1", staff.UserID, json.RawMessage(`{"language_code":"`+language+`"}`))
			require.NoError(t, err)
			f.H.peers.mu.Lock()
			f.H.peers.loomDown = true
			f.H.peers.mu.Unlock()
			f.H.start(t)
			var id uuid.UUID
			require.Eventually(t, func() bool {
				rows := specialistNotices(t, f, booking.ID)
				for _, n := range rows {
					if n.Kind == "soon" {
						id = n.ID
						return true
					}
				}
				return false
			}, 10*time.Second, 25*time.Millisecond)
			awaitSpecialistNotice(t, f, id, "delivered")
			messages := specialistMessages(f.H.peers)
			// Owner reminders use the same recipient for a staff self-booking. Identify the specialist notice by its heading.
			heading := "Massage session reminder"
			if language == "uk" {
				heading = "Напоминание о сессии массажа"
			}
			count := 0
			for _, message := range messages {
				if strings.Contains(message.Text, heading) {
					count++
					require.Contains(t, message.Text, booking.ID.String())
					require.Len(t, message.Entities, 1)
					require.Equal(t, "tg://user?id=11001", message.Entities[0].URL)
				}
			}
			require.Equal(t, 1, count)
			f.H.peers.mu.Lock()
			require.Empty(t, f.H.peers.createKeys)
			f.H.peers.mu.Unlock()
		})
	}
}
