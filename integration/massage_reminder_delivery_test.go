package integration_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func dueMassageReminder(t *testing.T) (paymentFixture, db.ProfileScope, db.MassageReminder) {
	t.Helper()
	f, owner, _, schedule := massageBookingFixture(t, 1)
	booking, _ := bookMassage(t, f, owner, massageBookingArgs(f, schedule, 0))
	settings, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.MassageNotifyClientPriorLongSeconds = 86400
	_, err = f.H.store.CompareAndSwapSettings(t.Context(), settings.Version, settings.Document, "reminder-fixture")
	require.NoError(t, err)
	return f, owner, bookingReminders(t, f, booking.ID)[db.MassageReminderLong]
}

func awaitMassageReminder(t *testing.T, f paymentFixture, id uuid.UUID, state string) db.MassageReminder {
	t.Helper()
	scope := db.EventScope{BotID: 123, Organization: "org"}
	var n db.MassageReminder
	require.Eventually(t, func() bool {
		var err error
		n, err = f.H.store.MassageReminder(t.Context(), scope, id)
		return err == nil && n.State == state
	}, 10*time.Second, 25*time.Millisecond)
	return n
}

func TestMassageReminderStoreFencesAttemptsAndRecoversUnknownWithoutReplay(t *testing.T) {
	t.Parallel()
	f, owner, n := dueMassageReminder(t)
	scope := db.EventScope{BotID: owner.BotID, Organization: owner.Organization}
	actor := paymentScope(t, f.H, owner.UserID, false)
	jobs, err := f.H.store.ReadyMassageReminders(t.Context(), scope, uuid.Nil)
	require.NoError(t, err)
	require.Equal(t, []db.NotificationJob{{ID: n.ID, Version: n.Version}}, jobs)
	reserved, err := f.H.store.ReserveMassageReminder(
		t.Context(),
		actor,
		n.ID,
		n.Version,
		json.RawMessage(`{"text":"Synthetic reminder"}`),
	)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, reserved.AttemptID)
	require.Equal(t, "send_inflight", reserved.State)
	_, err = f.H.store.ReserveMassageReminder(t.Context(), actor, n.ID, n.Version, json.RawMessage(`{}`))
	require.ErrorIs(t, err, db.ErrConflict)
	require.NoError(t, f.H.store.RecoverMassageReminders(t.Context(), scope))
	unknown := awaitMassageReminder(t, f, n.ID, "delivery_unknown")
	require.Equal(t, reserved.AttemptID, unknown.AttemptID)
	require.ErrorIs(t, f.H.store.FinishMassageReminder(t.Context(), reserved, "delivered", nil, 0), db.ErrConflict)
	require.NoError(t, f.H.store.RecoverMassageReminders(t.Context(), scope))
	require.Equal(t, unknown, awaitMassageReminder(t, f, n.ID, "delivery_unknown"))
	jobs, err = f.H.store.ReadyMassageReminders(t.Context(), scope, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, jobs)
	current, err := f.H.store.OwnMassageBooking(t.Context(), owner, n.Booking.EventID, n.Booking.ID)
	require.NoError(t, err)
	require.Equal(t, n.Booking, current)
}

func TestMassageReminderRetryRebasesSettingsAndKeepsBackoff(t *testing.T) {
	t.Parallel()
	f, owner, n := dueMassageReminder(t)
	actor := paymentScope(t, f.H, owner.UserID, false)
	scope := actor.EventScope
	reserved, err := f.H.store.ReserveMassageReminder(t.Context(), actor, n.ID, n.Version, json.RawMessage(`{}`))
	require.NoError(t, err)
	settings, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.MassageNotifyClientPriorLongSeconds = 1
	changed, err := f.H.store.CompareAndSwapSettings(
		t.Context(),
		settings.Version,
		settings.Document,
		"reminder-fixture",
	)
	require.NoError(t, err)
	require.NoError(t, f.H.store.FinishMassageReminder(t.Context(), reserved, "retryable", nil, time.Hour))
	retry := awaitMassageReminder(t, f, n.ID, "retryable")
	require.Equal(t, int32(1), retry.LeadSeconds)
	require.Equal(t, n.Booking.Snapshot.Terms.StartsAt.Add(-time.Second).UTC(), retry.DueAt)
	_, err = f.H.store.ReserveMassageReminder(t.Context(), actor, n.ID, retry.Version, json.RawMessage(`{}`))
	require.ErrorIs(t, err, db.ErrConflict)
	changed.Document.MassageNotifyClientPriorLongSeconds = 86400
	_, err = f.H.store.CompareAndSwapSettings(t.Context(), changed.Version, changed.Document, "reminder-fixture")
	require.NoError(t, err)
	after := awaitMassageReminder(t, f, n.ID, "retryable")
	require.Equal(t, retry.NextAttemptAt, after.NextAttemptAt)
	require.True(t, after.DueAt.Before(after.NextAttemptAt))
	jobs, err := f.H.store.ReadyMassageReminders(t.Context(), scope, uuid.Nil)
	require.NoError(t, err)
	require.Empty(t, jobs, "Earlier due cannot bypass retry-after")
}

func TestMassageReminderCancellationSuppressesRetryableFinishAndAdminRetry(t *testing.T) {
	t.Parallel()
	f, owner, n := dueMassageReminder(t)
	actor := paymentScope(t, f.H, owner.UserID, false)
	reserved, err := f.H.store.ReserveMassageReminder(t.Context(), actor, n.ID, n.Version, json.RawMessage(`{}`))
	require.NoError(t, err)
	args := db.MassageCancellationArguments{
		EventID:         f.Event.ID,
		BookingID:       n.Booking.ID,
		ExpectedVersion: n.Booking.Version,
	}
	op := uuid.New()
	proposal, err := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, args)
	require.NoError(t, err)
	_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, proposal.ID, args)
	require.NoError(t, err)
	require.NoError(t, f.H.store.FinishMassageReminder(t.Context(), reserved, "retryable", nil, 0))
	blocked := awaitMassageReminder(t, f, n.ID, "delivery_blocked")
	admin := paymentScope(t, f.H, f.AdminID, true)
	resolution := db.NotificationResolution{
		NotificationKind: "massage",
		EventID:          f.Event.ID,
		NotificationID:   n.ID,
		ExpectedVersion:  blocked.Version,
		Action:           "retry",
		Reason:           "Synthetic exact retry check",
	}
	prep, err := f.H.store.PrepareNotificationResolution(t.Context(), admin, "cancelled-reminder", resolution)
	require.NoError(t, err)
	_, err = f.H.store.ResolveNotification(t.Context(), admin, "cancelled-reminder", resolution, prep.ID)
	require.Error(t, err)
	require.Equal(t, blocked, awaitMassageReminder(t, f, n.ID, "delivery_blocked"))
}

func TestMassageReminderDeliveryUsesLanguageSnapshotAndDoesNotStartLoom(t *testing.T) {
	t.Parallel()
	f, owner, n := dueMassageReminder(t)
	p := f.H.peers
	_, err := f.H.store.Pool().
		Exec(
			t.Context(),
			"UPDATE shuttle_users SET profile=$2 WHERE id=$1",
			owner.UserID,
			json.RawMessage(`{"language_code":"uk"}`),
		)
	require.NoError(t, err)
	// Make both logical reminders due without coalescing them.
	settings, err := f.H.store.Settings(t.Context())
	require.NoError(t, err)
	settings.Document.MassageNotifyClientPriorSeconds = 86400
	_, err = f.H.store.CompareAndSwapSettings(t.Context(), settings.Version, settings.Document, "reminder-fixture")
	require.NoError(t, err)
	notices := bookingReminders(t, f, n.Booking.ID)
	f.H.start(t)
	for _, notice := range notices {
		awaitMassageReminder(t, f, notice.ID, "delivered")
	}
	texts := sentNotifications(p, 11002)
	require.Len(t, texts, 2)
	for _, text := range texts {
		require.Contains(t, text, "Напоминание о записи на массаж")
		require.Contains(t, text, n.Booking.Snapshot.EventName)
		require.Contains(t, text, n.Booking.Snapshot.Terms.Timezone)
		require.Contains(t, text, n.Booking.ID.String())
		require.NotContains(t, text, "через час")
	}
	p.mu.Lock()
	require.Empty(t, p.createKeys)
	p.mu.Unlock()
	f.H.stop(t)
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(p, 11002)) > 2 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
}

func TestMassageReminderUnknownRequiresExactAuthorizedResolution(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"retry", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f, _, n := dueMassageReminder(t)
			p := f.H.peers
			p.mu.Lock()
			p.loseSend = true
			p.mu.Unlock()
			f.H.start(t)
			unknown := awaitMassageReminder(t, f, n.ID, "delivery_unknown")
			f.H.stop(t)
			require.Len(t, sentNotifications(p, 11002), 1)
			f.H.start(t)
			require.Never(
				t,
				func() bool { return len(sentNotifications(p, 11002)) > 1 },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
			f.H.stop(t)
			args := db.NotificationResolution{
				NotificationKind: "massage",
				EventID:          f.Event.ID,
				NotificationID:   n.ID,
				ExpectedVersion:  unknown.Version,
				Action:           action,
				Reason:           "Checked synthetic receipt/chat; last resort",
			}
			body := preparedNotification(t, f, "massage-reminder-resolution", args)
			for _, token := range []string{f.Owner, f.Ambassador} {
				code, _ := adminRequest(t, f.H, token, http.MethodPost, notificationResolutionAPI, body)
				require.Equal(t, http.StatusForbidden, code)
			}
			resolved := resolvedNotification(t, f, body)
			require.Equal(t, "massage", resolved.NotificationKind)
			f.H.start(t)
			if action == "retry" {
				awaitMassageReminder(t, f, n.ID, "delivered")
				require.Len(t, sentNotifications(p, 11002), 2)
			} else {
				awaitMassageReminder(t, f, n.ID, "dismissed")
				require.Len(t, sentNotifications(p, 11002), 1)
			}
			require.Equal(t, resolved, resolvedNotification(t, f, body))
			code, raw := agentAdminRead(t, f.H, f.Admin, "massage_reminder", map[string]any{"id": n.ID})
			require.Equal(t, http.StatusOK, code, string(raw))
			require.NotContains(t, string(raw), `"payload"`)
			code, raw = agentAdminRead(t, f.H, f.Admin, "massage_reminders", map[string]any{"limit": 1})
			require.Equal(t, http.StatusOK, code, string(raw))
			require.Contains(t, string(raw), `"due_at"`)
			code, raw = agentAdminRead(t, f.H, f.Owner, "massage_reminders", map[string]any{})
			require.Equal(t, http.StatusForbidden, code, string(raw))
		})
	}
}

func TestMassageReminderDeliveryRechecksLocalChangesAfterIAMWait(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"cancel", "module", "revoked", "postponed"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, owner, n := dueMassageReminder(t)
			p := f.H.peers
			entered, release := make(chan struct{}), make(chan struct{})
			var signal, unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(release) }) })
			p.tokenHook = func(subject string) {
				if subject == owner.Subject {
					signal.Do(func() { close(entered); <-release })
				}
			}
			f.H.start(t)
			select {
			case <-entered:
			case <-time.After(8 * time.Second):
				t.Fatal("reminder did not reach IAM wait")
			}
			var err error
			switch change {
			case "cancel":
				args := db.MassageCancellationArguments{
					EventID:         f.Event.ID,
					BookingID:       n.Booking.ID,
					ExpectedVersion: n.Booking.Version,
				}
				op := uuid.New()
				prep, e := f.H.store.PrepareOwnMassageCancellation(t.Context(), owner, op, args)
				require.NoError(t, e)
				_, err = f.H.store.CancelOwnMassage(t.Context(), owner, op, prep.ID, args)
			case "module":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
			case "revoked":
				_, err = f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", owner.UserID)
			case "postponed":
				settings, e := f.H.store.Settings(t.Context())
				require.NoError(t, e)
				settings.Document.MassageNotifyClientPriorLongSeconds = 1
				_, err = f.H.store.CompareAndSwapSettings(
					t.Context(),
					settings.Version,
					settings.Document,
					"reminder-fixture",
				)
			}
			require.NoError(t, err)
			unblock.Do(func() { close(release) })
			if change == "postponed" {
				// Wait for the stale queued generation to finish, not just for CAS's pending row.
				require.Eventually(t, func() bool {
					var count int
					readErr := f.H.store.Pool().
						QueryRow(t.Context(), "SELECT count(*) FROM river_job WHERE kind='zinusya_massage_reminder' AND "+
							"state='completed'").
						Scan(&count)
					return readErr == nil && count > 0
				}, 8*time.Second, 25*time.Millisecond)
				awaitMassageReminder(t, f, n.ID, "pending")
			} else {
				awaitMassageReminder(t, f, n.ID, "delivery_blocked")
			}
			require.Empty(t, sentNotifications(p, 11002))
		})
	}
}

func TestMassageReminderDeliveryWaitsForIAMAndRetriesOnlyKnownRejection(t *testing.T) {
	t.Parallel()
	f, owner, n := dueMassageReminder(t)
	p := f.H.peers
	entered := make(chan struct{}, 1)
	p.unavailableTokenSubjects = map[string]bool{owner.Subject: true}
	p.tokenHook = func(subject string) {
		if subject == owner.Subject {
			select {
			case entered <- struct{}{}:
			default:
			}
		}
	}
	f.H.start(t)
	select {
	case <-entered:
	case <-time.After(8 * time.Second):
		t.Fatal("reminder did not attempt IAM")
	}
	pending := awaitMassageReminder(t, f, n.ID, "pending")
	require.Equal(t, uuid.Nil, pending.AttemptID)
	f.H.stop(t)
	p.mu.Lock()
	p.unavailableTokenSubjects = nil
	p.rejectSendOnce = true
	p.loomDown = true
	p.mu.Unlock()
	f.H.start(t)
	awaitMassageReminder(t, f, n.ID, "retryable")
	awaitMassageReminder(t, f, n.ID, "delivered")
	require.Len(t, sentNotifications(p, 11002), 2)
	p.mu.Lock()
	require.Empty(t, p.createKeys)
	p.mu.Unlock()
}

func TestMassageReminderFailedReceiptWriteBecomesUnknownWithoutAnotherSend(t *testing.T) {
	t.Parallel()
	f, _, n := dueMassageReminder(t)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "CREATE FUNCTION fail_reminder_delivered_receipt() RETURNS trigger LANGUAGE "+
			"plpgsql AS $$ BEGIN RAISE EXCEPTION 'synthetic reminder delivered receipt "+
			"failure'; END $$;\n"+
			"CREATE TRIGGER fail_reminder_delivered_receipt BEFORE UPDATE ON "+
			"shuttle_massage_reminders FOR EACH ROW WHEN (NEW.state = 'delivered') EXECUTE "+
			"FUNCTION fail_reminder_delivered_receipt()")
	require.NoError(t, err)
	f.H.start(t)
	unknown := awaitMassageReminder(t, f, n.ID, "delivery_unknown")
	require.NotEqual(t, uuid.Nil, unknown.AttemptID)
	require.Len(t, sentNotifications(f.H.peers, 11002), 1)
	f.H.stop(t)
	f.H.start(t)
	require.Never(
		t,
		func() bool { return len(sentNotifications(f.H.peers, 11002)) != 1 },
		1200*time.Millisecond,
		25*time.Millisecond,
	)
	require.Equal(t, unknown, awaitMassageReminder(t, f, n.ID, "delivery_unknown"))
}
