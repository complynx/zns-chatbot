package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/payments"
)

func TestMassageSpecialistDeliveryRetriesOnlyKnownAttempts(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	id := specialistNotices(t, f, booking.ID)[0].ID
	scope := paymentScope(t, f.H, staff.UserID, false)
	ready, err := f.H.store.ReadyMassageSpecialistNotifications(t.Context(), scope.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, []db.NotificationJob{{ID: id, Version: 1}}, ready)
	u, err := f.H.store.MassageSpecialistNotificationRecipient(t.Context(), scope.EventScope, id)
	require.NoError(t, err)
	require.Equal(t, staff.UserID, u.ID, "A bound specialist does not need a participant pass")
	payload := json.RawMessage(`{"text":"booking"}`)
	first, err := f.H.store.ReserveMassageSpecialistNotification(t.Context(), scope, id, 1, payload)
	require.NoError(t, err)
	require.Equal(t, "send_inflight", first.State)
	require.NotEqual(t, uuid.Nil, first.AttemptID)
	require.JSONEq(t, string(payload), string(first.Payload))
	require.NoError(
		t,
		f.H.store.FinishMassageSpecialistNotification(
			t.Context(),
			first,
			"retryable",
			json.RawMessage(`{"known_not_sent":true}`),
			0,
		),
	)
	require.ErrorIs(
		t,
		f.H.store.FinishMassageSpecialistNotification(t.Context(), first, "delivered", nil, 0),
		db.ErrConflict,
	)
	retry, err := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, id)
	require.NoError(t, err)
	require.EqualValues(t, 3, retry.Version)
	second, err := f.H.store.ReserveMassageSpecialistNotification(t.Context(), scope, id, retry.Version, payload)
	require.NoError(t, err)
	require.NotEqual(t, first.AttemptID, second.AttemptID)
	require.NoError(
		t,
		f.H.store.FinishMassageSpecialistNotification(
			t.Context(),
			second,
			"delivered",
			json.RawMessage(`{"message_id":123}`),
			0,
		),
	)
	saved, err := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, id)
	require.NoError(t, err)
	require.Equal(t, "delivered", saved.State)
	require.JSONEq(t, `{"message_id":123}`, string(saved.Receipt))
	require.Equal(t, second.AttemptID, saved.AttemptID)
	ready, err = f.H.store.ReadyMassageSpecialistNotifications(t.Context(), scope.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, ready)
}

func TestMassageSpecialistDeliveryRechecksAuthorityAfterCallerReads(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"mute", "mute_reenable", "regrant", "revoked_user", "disabled_module", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, staff, schedule, binding := staffFixture(t)
			owner := ownerProfileScope(t, f.H, f.OwnerID)
			booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
			id := specialistNotices(t, f, booking.ID)[0].ID
			scope := paymentScope(t, f.H, staff.UserID, false)
			_, err := f.H.store.MassageSpecialistNotificationRecipient(t.Context(), scope.EventScope, id)
			require.NoError(t, err)
			switch change {
			case "mute", "mute_reenable":
				_, err = f.H.store.SetMassageSpecialistPreferences(
					t.Context(),
					staff,
					uuid.New(),
					noticePreference(f.Event.ID, 0, false),
				)
				require.NoError(t, err)
				if change == "mute_reenable" {
					_, err = f.H.store.SetMassageSpecialistPreferences(
						t.Context(),
						staff,
						uuid.New(),
						noticePreference(f.Event.ID, 1, true),
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
			case "disabled_module":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
				require.NoError(t, err)
			case "cancelled":
				cancelMassageForTest(t, f, owner, booking)
			}
			_, err = f.H.store.ReserveMassageSpecialistNotification(t.Context(), scope, id, 1, json.RawMessage(`{}`))
			require.Error(t, err)
			require.True(t, errors.Is(err, pgx.ErrNoRows) || errors.Is(err, payments.ErrUnavailable))
			saved, e := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, id)
			require.NoError(t, e)
			require.Equal(t, uuid.Nil, saved.AttemptID)
			require.Empty(t, saved.Payload)
			require.NoError(
				t,
				f.H.store.BlockMassageSpecialistNotification(t.Context(), scope.EventScope, id, saved.Version),
			)
			blocked, e := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, id)
			require.NoError(t, e)
			require.Equal(t, "delivery_blocked", blocked.State)
		})
	}
}

func TestMassageSpecialistDeliveryRecoveryPreservesUnknownAttemptAndCancellation(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	id := specialistNotices(t, f, booking.ID)[0].ID
	scope := paymentScope(t, f.H, staff.UserID, false)
	reserved, err := f.H.store.ReserveMassageSpecialistNotification(
		t.Context(),
		scope,
		id,
		1,
		json.RawMessage(`{"text":"possibly sent"}`),
	)
	require.NoError(t, err)
	cancelMassageForTest(t, f, owner, booking)
	// Recovery is called only under the application's exclusive instance lock.
	require.NoError(
		t,
		f.H.store.RecoverMassageSpecialistNotifications(
			t.Context(),
			db.EventScope{BotID: scope.BotID + 1, Organization: scope.Organization},
		),
	)
	current, err := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, id)
	require.NoError(t, err)
	require.Equal(t, "send_inflight", current.State)
	require.NoError(t, f.H.store.RecoverMassageSpecialistNotifications(t.Context(), scope.EventScope))
	current, err = f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, id)
	require.NoError(t, err)
	require.Equal(t, "delivery_unknown", current.State)
	require.Equal(t, reserved.AttemptID, current.AttemptID)
	require.Equal(t, reserved.Payload, current.Payload)
	require.ErrorIs(
		t,
		f.H.store.FinishMassageSpecialistNotification(t.Context(), reserved, "delivered", nil, 0),
		db.ErrConflict,
	)
	ready, err := f.H.store.ReadyMassageSpecialistNotifications(t.Context(), scope.EventScope, uuid.Nil)
	require.NoError(t, err)
	require.Len(t, ready, 1)
	cancelled, err := f.H.store.MassageSpecialistNotification(t.Context(), scope.EventScope, ready[0].ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", cancelled.Kind)
	require.Equal(t, booking.ID, cancelled.Booking.ID)
	_, err = f.H.store.ReserveMassageSpecialistNotification(
		t.Context(),
		scope,
		cancelled.ID,
		cancelled.Version,
		json.RawMessage(`{"text":"cancelled"}`),
	)
	require.NoError(t, err, "Cancellation delivery does not require a currently booked row")
}

func TestMassageSpecialistDeliveryScopesAndConcurrentReservations(t *testing.T) {
	t.Parallel()
	f, staff, schedule, _ := staffFixture(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	id := specialistNotices(t, f, booking.ID)[0].ID
	scope := paymentScope(t, f.H, staff.UserID, false)
	for _, foreign := range []db.EventScope{{BotID: scope.BotID + 1, Organization: scope.Organization}, {BotID: scope.BotID, Organization: "foreign"}} {
		_, err := f.H.store.MassageSpecialistNotification(t.Context(), foreign, id)
		require.ErrorIs(t, err, pgx.ErrNoRows)
		_, err = f.H.store.MassageSpecialistNotificationRecipient(t.Context(), foreign, id)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	_, err := f.H.store.ReserveMassageSpecialistNotification(
		t.Context(),
		paymentScope(t, f.H, owner.UserID, false),
		id,
		1,
		json.RawMessage(`{}`),
	)
	require.ErrorIs(t, err, payments.ErrDenied)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, e := f.H.store.ReserveMassageSpecialistNotification(ctx, scope, id, 1, json.RawMessage(`{}`))
			results <- e
		}()
	}
	close(start)
	successes, conflicts := 0, 0
	for range 2 {
		e := <-results
		switch {
		case e == nil:
			successes++
		case errors.Is(e, db.ErrConflict):
			conflicts++
		default:
			require.NoError(t, e)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}
