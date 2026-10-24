package integration_test

import (
	"encoding/json"
	"maps"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Keep the administrator distinct from the notice recipient so recipient
// revocation does not also revoke the operator used by these tests.
func unknownSpecialistNotice(
	t *testing.T,
) (paymentFixture, db.ProfileScope, db.MassageSpecialistNotification, db.MassageBindingChange) {
	t.Helper()
	f, admin, schedule, binding := staffFixture(t)
	binding.SpecialistID = "beth"
	binding.ExpectedVersion = 0
	binding.UserID = f.AmbassadorID
	changed := bindingChange(t, f, admin, binding)
	binding.ExpectedVersion = changed.Version
	staff := ownerProfileScope(t, f.H, f.AmbassadorID)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	_, err := f.H.store.Pool().
		Exec(
			t.Context(),
			"UPDATE shuttle_users SET profile=$2 WHERE id=$1",
			owner.UserID,
			json.RawMessage(`{"first_name":"Hidden Client"}`),
		)
	require.NoError(t, err)
	args := massageBookingArgs(f, schedule, 0)
	args.Selection.SpecialistID = "beth"
	booking, _ := bookMassage(t, f, owner, args)
	id := specialistNotices(t, f, booking.ID)[0].ID
	f.H.peers.mu.Lock()
	f.H.peers.loseSend = true
	f.H.peers.mu.Unlock()
	f.H.start(t)
	unknown := awaitSpecialistNotice(t, f, id, "delivery_unknown")
	f.H.stop(t)
	require.Len(t, sentNotifications(f.H.peers, 11003), 1)
	return f, staff, unknown, binding
}

func specialistResolution(n db.MassageSpecialistNotification, action string) db.NotificationResolution {
	return db.NotificationResolution{
		NotificationKind: "massage_specialist",
		EventID:          n.Booking.EventID,
		NotificationID:   n.ID,
		ExpectedVersion:  n.Version,
		Action:           action,
		Reason:           "Checked receipt and synthetic chat; last resort if retry",
	}
}

func TestMassageSpecialistAdministrationResolvesExactlyAndPreservesOriginalReceipt(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"retry", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			f, _, unknown, _ := unknownSpecialistNotice(t)
			args := specialistResolution(unknown, action)
			body := preparedNotification(t, f, "specialist-resolution", args)
			for _, token := range []string{f.Owner, f.Ambassador} {
				code, _ := adminRequest(t, f.H, token, http.MethodPost, notificationResolutionAPI, body)
				require.Equal(t, http.StatusForbidden, code)
			}
			result := resolvedNotification(t, f, body)
			require.Equal(t, "massage_specialist", result.NotificationKind)
			require.Equal(t, unknown.RecipientID, result.RecipientID)
			require.JSONEq(t, string(unknown.Receipt), string(result.PreviousReceipt))
			require.Equal(t, "delivery_unknown", result.PreviousState)
			require.Equal(t, unknown.Version+1, result.Version)
			var target uuid.UUID
			var targets int
			require.NoError(
				t,
				f.H.store.Pool().
					QueryRow(t.Context(), "SELECT massage_specialist_notification_id,num_nonnulls(notification_id,"+
						"pair_notification_id,order_notification_id,massage_reminder_id,"+
						"massage_specialist_notification_id) FROM shuttle_notification_resolutions WHERE "+
						"operation_id='specialist-resolution'").
					Scan(&target, &targets),
			)
			require.Equal(t, unknown.ID, target)
			require.Equal(t, 1, targets)
			_, err := f.H.store.Pool().
				Exec(t.Context(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval "+
					"'1 second' WHERE operation_id='specialist-resolution'")
			require.NoError(t, err)
			f.H.start(t)
			expected := 1
			if action == "retry" {
				awaitSpecialistNotice(t, f, unknown.ID, "delivered")
				expected = 2
			} else {
				awaitSpecialistNotice(t, f, unknown.ID, "dismissed")
			}
			require.Never(
				t,
				func() bool { return len(sentNotifications(f.H.peers, 11003)) != expected },
				1200*time.Millisecond,
				25*time.Millisecond,
			)
			require.Equal(t, result, resolvedNotification(t, f, body))
			for _, suffix := range []string{"status", "reconcile"} {
				code, raw := adminRequest(
					t,
					f.H,
					f.Admin,
					http.MethodPost,
					notificationResolutionAPI+"/"+suffix,
					map[string]any{"protocol_version": 1, "operation_id": "specialist-resolution"},
				)
				require.Equal(t, http.StatusOK, code, string(raw))
				var response struct {
					Result db.NotificationResolutionResult `json:"result"`
				}
				require.NoError(t, json.Unmarshal(raw, &response))
				require.Equal(t, result, response.Result)
			}
			saved, err := f.H.store.MassageSpecialistNotification(
				t.Context(),
				db.EventScope{BotID: 123, Organization: "org"},
				unknown.ID,
			)
			require.NoError(t, err)
			require.Equal(t, unknown.BindingVersion, saved.BindingVersion)
			require.Equal(t, unknown.PreferenceVersion, saved.PreferenceVersion)
			booking, err := f.H.store.OwnMassageBooking(
				t.Context(),
				ownerProfileScope(t, f.H, f.OwnerID),
				f.Event.ID,
				unknown.Booking.ID,
			)
			require.NoError(t, err)
			require.Equal(t, unknown.Booking, booking)
			var reviews int
			require.NoError(
				t,
				f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_payment_reviews").Scan(&reviews),
			)
			require.Zero(t, reviews)
		})
	}
}

func TestMassageSpecialistAdministrationDiagnosticsAreScopedAndExcludeContacts(t *testing.T) {
	t.Parallel()
	f, _, unknown, _ := unknownSpecialistNotice(t)
	owner := ownerProfileScope(t, f.H, f.OwnerID)
	cancelMassageForTest(t, f, owner, unknown.Booking)
	seen := map[uuid.UUID]bool{}
	after := uuid.Nil
	for range 3 {
		args := map[string]any{"limit": 1}
		if after != uuid.Nil {
			args["after"] = after
		}
		code, raw := agentAdminRead(t, f.H, f.Admin, "massage_specialist_notifications", args)
		require.Equal(t, http.StatusOK, code, string(raw))
		require.NotContains(t, string(raw), "Hidden Client")
		require.NotContains(t, string(raw), "tg://")
		require.NotContains(t, string(raw), `"payload"`)
		var response struct {
			Result struct {
				Notifications []db.MassageSpecialistNotificationSummary `json:"notifications"`
				Next          *uuid.UUID                                `json:"next"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &response))
		for _, row := range response.Result.Notifications {
			require.False(t, seen[row.ID])
			seen[row.ID] = true
			require.Equal(t, f.Event.ID, row.EventID)
		}
		if response.Result.Next == nil {
			break
		}
		after = *response.Result.Next
	}
	require.Len(t, seen, 2)
	code, raw := agentAdminRead(t, f.H, f.Admin, "massage_specialist_notification", map[string]any{"id": unknown.ID})
	require.Equal(t, http.StatusOK, code, string(raw))
	for _, forbidden := range []string{"Hidden Client", "tg://", `"payload"`, `"client_name"`, `"profile"`, `"contact"`} {
		require.NotContains(t, string(raw), forbidden)
	}
	var response struct {
		Result db.MassageSpecialistNotificationDetail `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Equal(t, unknown.AttemptID, response.Result.AttemptID)
	require.JSONEq(t, string(unknown.Receipt), string(response.Result.Receipt))
	require.Equal(t, unknown.BindingVersion, response.Result.BindingVersion)
	for _, token := range []string{f.Owner, f.Ambassador} {
		code, raw = agentAdminRead(t, f.H, token, "massage_specialist_notifications", map[string]any{})
		require.Equal(t, http.StatusForbidden, code, string(raw))
		code, raw = agentAdminRead(t, f.H, token, "massage_specialist_notification", map[string]any{"id": unknown.ID})
		require.Equal(t, http.StatusForbidden, code, string(raw))
	}
	for _, scope := range []db.EventScope{{BotID: 124, Organization: "org"}, {BotID: 123, Organization: "other"}} {
		rows, err := f.H.store.ListAdminMassageSpecialistNotifications(t.Context(), scope, "", uuid.Nil, 20)
		require.NoError(t, err)
		require.Empty(t, rows)
		_, err = f.H.store.AdminMassageSpecialistNotification(t.Context(), scope, unknown.ID)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	code, raw = agentAdminRead(
		t,
		f.H,
		f.Admin,
		"massage_specialist_notifications",
		map[string]any{"state": "delivery_unknown"},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	var filtered struct {
		Result struct {
			Notifications []db.MassageSpecialistNotificationSummary `json:"notifications"`
		} `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &filtered))
	require.Len(t, filtered.Result.Notifications, 1)
	require.Equal(t, unknown.ID, filtered.Result.Notifications[0].ID)
	require.Equal(t, "delivery_unknown", filtered.Result.Notifications[0].State)
	code, _ = agentAdminRead(t, f.H, f.Admin, "massage_specialist_notifications", map[string]any{"limit": 21})
	require.Equal(t, http.StatusUnprocessableEntity, code)
}

func TestMassageSpecialistAdministrationRetryCannotReviveStaleGrantOrPreference(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"mute_reenable", "regrant", "module", "recipient_revoked", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, staff, unknown, binding := unknownSpecialistNotice(t)
			body := preparedNotification(t, f, "stale-specialist-retry", specialistResolution(unknown, "retry"))
			switch change {
			case "mute_reenable":
				_, err := f.H.store.SetMassageSpecialistPreferences(
					t.Context(),
					staff,
					uuid.New(),
					noticePreference(f.Event.ID, 0, false),
				)
				require.NoError(t, err)
				_, err = f.H.store.SetMassageSpecialistPreferences(
					t.Context(),
					staff,
					uuid.New(),
					noticePreference(f.Event.ID, 1, true),
				)
				require.NoError(t, err)
			case "regrant":
				admin := ownerProfileScope(t, f.H, f.AdminID)
				binding.Active = false
				revoked := bindingChange(t, f, admin, binding)
				binding.Active = true
				binding.ExpectedVersion = revoked.Version
				bindingChange(t, f, admin, binding)
			case "module":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
				require.NoError(t, err)
			case "recipient_revoked":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_users SET revoked=true WHERE id=$1", staff.UserID)
				require.NoError(t, err)
			case "cancelled":
				cancelMassageForTest(t, f, ownerProfileScope(t, f.H, f.OwnerID), unknown.Booking)
			}
			code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI, body)
			assertSpecialistResolutionRejected(t, code, raw)
			current := awaitSpecialistNotice(t, f, unknown.ID, "delivery_unknown")
			require.Equal(t, unknown.Version, current.Version)
			require.Equal(t, unknown.AttemptID, current.AttemptID)
			require.Equal(t, unknown.Payload, current.Payload)
			require.Equal(t, unknown.BindingVersion, current.BindingVersion)
			require.Equal(t, unknown.PreferenceVersion, current.PreferenceVersion)
			admin := paymentScope(t, f.H, f.AdminID, true)
			_, err := f.H.store.NotificationResolutionReceipt(t.Context(), admin, "stale-specialist-retry")
			require.ErrorIs(t, err, pgx.ErrNoRows)
			dismissed := resolvedNotification(
				t,
				f,
				preparedNotification(t, f, "dismiss-stale-specialist", specialistResolution(unknown, "dismiss")),
			)
			require.Equal(t, "dismissed", dismissed.State)
			require.Len(t, sentNotifications(f.H.peers, 11003), 1)
		})
	}
}

func TestMassageSpecialistAdministrationChecksExactAndUnexpiredProposal(t *testing.T) {
	t.Parallel()
	f, _, unknown, _ := unknownSpecialistNotice(t)
	args := specialistResolution(unknown, "retry")
	body := preparedNotification(t, f, "specialist-proposal-guards", args)
	changed := args
	changed.Reason = "Changed after preparation"
	altered := maps.Clone(body)
	altered["arguments"] = changed
	code, raw := adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI, altered)
	assertSpecialistResolutionRejected(t, code, raw)
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_notification_resolutions SET expires_at=clock_timestamp()-interval "+
			"'1 second' WHERE operation_id='specialist-proposal-guards'")
	require.NoError(t, err)
	code, raw = adminRequest(t, f.H, f.Admin, http.MethodPost, notificationResolutionAPI, body)
	assertSpecialistResolutionRejected(t, code, raw)
	wrongEvent := args
	wrongEvent.EventID = uuid.New()
	_, err = f.H.store.PrepareNotificationResolution(
		t.Context(),
		paymentScope(t, f.H, f.AdminID, true),
		"foreign-event-specialist",
		wrongEvent,
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	wrongKind := args
	wrongKind.NotificationKind = "massage"
	_, err = f.H.store.PrepareNotificationResolution(
		t.Context(),
		paymentScope(t, f.H, f.AdminID, true),
		"wrong-kind-specialist",
		wrongKind,
	)
	require.ErrorIs(t, err, pgx.ErrNoRows)
	require.Equal(t, unknown, awaitSpecialistNotice(t, f, unknown.ID, "delivery_unknown"))
}

func assertSpecialistResolutionRejected(t *testing.T, code int, raw []byte) {
	t.Helper()
	require.Equal(t, http.StatusOK, code, string(raw))
	var result struct {
		Status string                           `json:"status"`
		Result *db.NotificationResolutionResult `json:"result"`
		Error  struct {
			Code           string `json:"code"`
			Classification string `json:"classification"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(raw, &result))
	require.Equal(t, "rejected", result.Status)
	require.Nil(t, result.Result)
	require.Equal(t, "notification_resolution_rejected", result.Error.Code)
	require.Equal(t, "terminal_rejection", result.Error.Classification)
}
