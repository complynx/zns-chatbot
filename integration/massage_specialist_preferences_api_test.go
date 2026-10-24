package integration_test

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

const preferenceAPI = "/internal/agent/massage_staff/"

func preferenceAgentWrite(t *testing.T, f paymentFixture, token string, op uuid.UUID, args any) (int, []byte) {
	t.Helper()
	return adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		preferenceAPI+"set_preferences",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     op,
			"idempotency_key":  op,
			"arguments":        args,
			"confirmation":     map[string]any{"mode": "none"},
		},
	)
}

func preferenceResult(t *testing.T, code int, raw []byte) db.MassageSpecialistPreferences {
	t.Helper()
	require.Equal(t, http.StatusOK, code, string(raw))
	var response struct {
		Status string                           `json:"status"`
		Result *db.MassageSpecialistPreferences `json:"result"`
	}
	require.NoError(t, json.Unmarshal(raw, &response))
	require.Equal(t, "succeeded", response.Status, string(raw))
	require.NotNil(t, response.Result)
	return *response.Result
}

func TestMassageSpecialistPreferenceHTTPSharedReceiptsAndBareLoomStatus(t *testing.T) {
	t.Parallel()
	f, staff, token, _, session := staffHTTPFixture(t)
	path := "/app/massage-staff/" + f.Event.ID.String()
	initial := massageAppCall[db.MassageSpecialistPreferences](t, f, session, http.MethodGet, path+"/preferences", nil)
	require.Equal(
		t,
		db.MassageSpecialistPreferences{UserID: staff.UserID, NotifyBookings: true, NotifyNext: true},
		initial,
	)
	agentInitial := staffAgentRead[db.MassageSpecialistPreferences](
		t,
		f,
		token,
		"preferences",
		map[string]any{"event_id": f.Event.ID},
	)
	require.Equal(t, initial, agentInitial)
	var before int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&before))
	operation := uuid.New()
	args := noticePreference(f.Event.ID, 0, false)
	code, raw := preferenceAgentWrite(t, f, token, operation, args)
	disabled := preferenceResult(t, code, raw)
	require.False(t, disabled.NotifyBookings)
	require.EqualValues(t, 1, disabled.Version)
	replay := massageAppCall[db.MassageSpecialistPreferences](
		t,
		f,
		session,
		http.MethodPost,
		path+"/preferences",
		map[string]any{"operation_id": operation, "arguments": args},
	)
	require.Equal(t, disabled, replay)
	second := uuid.New()
	enabled := massageAppCall[db.MassageSpecialistPreferences](
		t,
		f,
		session,
		http.MethodPost,
		path+"/preferences",
		map[string]any{"operation_id": second, "arguments": noticePreference(f.Event.ID, 1, true)},
	)
	require.True(t, enabled.NotifyBookings)
	require.EqualValues(t, 2, enabled.Version)
	// Loom status sends only protocol_version and operation_id, not event arguments.
	for _, suffix := range []string{"status", "reconcile"} {
		code, raw = adminRequest(
			t,
			f.H,
			token,
			http.MethodPost,
			preferenceAPI+"set_preferences/"+suffix,
			map[string]any{"protocol_version": 1, "operation_id": operation},
		)
		require.Equal(t, disabled, preferenceResult(t, code, raw))
	}
	nativeReceipt := massageAppCall[db.MassageSpecialistPreferences](
		t,
		f,
		session,
		http.MethodGet,
		path+"/preference-operations/"+operation.String(),
		nil,
	)
	require.Equal(t, disabled, nativeReceipt)
	code, raw = preferenceAgentWrite(t, f, token, operation, args)
	require.Equal(t, disabled, preferenceResult(t, code, raw))
	code, raw = preferenceAgentWrite(t, f, token, second, noticePreference(f.Event.ID, 1, true))
	require.Equal(t, enabled, preferenceResult(t, code, raw))
	current := staffAgentRead[db.MassageSpecialistPreferences](
		t,
		f,
		token,
		"preferences",
		map[string]any{"event_id": f.Event.ID},
	)
	require.Equal(t, enabled, current)
	secondEvent := anotherStaffEvent(t, f)
	global := staffAgentRead[db.MassageSpecialistPreferences](
		t,
		f,
		token,
		"preferences",
		map[string]any{"event_id": secondEvent},
	)
	require.Equal(t, enabled, global)
	var after, operations int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&after))
	require.Equal(t, before, after)
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(
				t.Context(),
				"SELECT count(*) FROM shuttle_massage_specialist_preference_operations WHERE user_id=$1",
				staff.UserID,
			).
			Scan(&operations),
	)
	require.Equal(t, 2, operations)
	f.H.peers.mu.Lock()
	require.Empty(t, f.H.peers.createKeys)
	f.H.peers.mu.Unlock()
}

func TestMassageSpecialistPreferenceHTTPRejectsMissingBooleanCASAndWrongScope(t *testing.T) {
	t.Parallel()
	f, staff, token, _, session := staffHTTPFixture(t)
	path := "/app/massage-staff/" + f.Event.ID.String() + "/preferences"
	for _, other := range []string{f.Owner, f.Admin} {
		code, raw := adminRequest(
			t,
			f.H,
			other,
			http.MethodPost,
			preferenceAPI+"preferences",
			map[string]any{
				"protocol_version": 1,
				"operation_id":     "read",
				"arguments":        map[string]any{"event_id": f.Event.ID},
			},
		)
		require.Equal(t, http.StatusNotFound, code, string(raw))
		code, raw = preferenceAgentWrite(t, f, other, uuid.New(), noticePreference(f.Event.ID, 0, false))
		require.Equal(t, http.StatusOK, code, string(raw))
		require.Contains(t, string(raw), `"status":"rejected"`)
	}
	for _, args := range []any{
		map[string]any{"event_id": f.Event.ID, "expected_version": 0},
		map[string]any{"event_id": f.Event.ID, "expected_version": 0, "notify_bookings": nil},
	} {
		code, raw := preferenceAgentWrite(t, f, token, uuid.New(), args)
		if code == http.StatusOK {
			require.Contains(t, string(raw), `"status":"rejected"`)
		} else {
			require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
		}
		code, _, _ = appCall(
			t,
			f.H,
			http.MethodPost,
			path,
			paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "arguments": args}),
			session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code)
	}
	for _, args := range []any{
		map[string]any{"event_id": f.Event.ID, "notify_bookings": false},
		map[string]any{"event_id": f.Event.ID, "notify_bookings": false, "expected_version": nil},
	} {
		code, raw := preferenceAgentWrite(t, f, token, uuid.New(), args)
		require.Equal(t, http.StatusUnprocessableEntity, code, string(raw))
		code, _, _ = appCall(
			t,
			f.H,
			http.MethodPost,
			path,
			paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "arguments": args}),
			session.Token,
			appTestOrigin,
		)
		require.Equal(t, http.StatusBadRequest, code)
	}
	op := uuid.New()
	code, raw := preferenceAgentWrite(t, f, token, op, noticePreference(f.Event.ID, 0, false))
	saved := preferenceResult(t, code, raw)
	code, raw = preferenceAgentWrite(t, f, token, uuid.New(), noticePreference(f.Event.ID, 0, true))
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	code, raw = preferenceAgentWrite(t, f, token, op, noticePreference(f.Event.ID, 0, true))
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path,
		paymentAppJSON(
			t,
			map[string]any{"operation_id": uuid.New(), "arguments": noticePreference(f.Event.ID, 0, true)},
		),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code)
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path,
		paymentAppJSON(
			t,
			map[string]any{"operation_id": uuid.New(), "arguments": noticePreference(uuid.New(), 1, true)},
		),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusBadRequest, code)
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path,
		paymentAppJSON(
			t,
			map[string]any{"operation_id": uuid.New(), "arguments": noticePreference(f.Event.ID, 1, true)},
		),
		session.Token,
		"https://foreign.invalid",
	)
	require.Equal(t, http.StatusForbidden, code)
	ownerSession := detailsAppSession(t, f.H, 11002, 234)
	code, _, _ = appCall(t, f.H, http.MethodGet, path, "", ownerSession.Token, appTestOrigin)
	require.Equal(t, http.StatusNotFound, code)
	code, raw = adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		preferenceAPI+"set_preferences",
		map[string]any{
			"protocol_version": 1,
			"operation_id":     uuid.New(),
			"idempotency_key":  uuid.New(),
			"arguments":        noticePreference(f.Event.ID, 1, true),
		},
	)
	require.Equal(t, http.StatusBadRequest, code, string(raw))
	for _, scope := range []db.ProfileScope{
		{BotID: 124, Organization: staff.Organization, UserID: staff.UserID, Subject: staff.Subject},
		{BotID: 123, Organization: "foreign", UserID: staff.UserID, Subject: staff.Subject},
	} {
		_, err := f.H.store.OwnMassageSpecialistPreferenceOperation(t.Context(), scope, op)
		require.ErrorIs(t, err, pgx.ErrNoRows)
	}
	require.Equal(
		t,
		saved,
		staffAgentRead[db.MassageSpecialistPreferences](
			t,
			f,
			token,
			"preferences",
			map[string]any{"event_id": f.Event.ID},
		),
	)
}

func TestMassageSpecialistPreferenceHTTPStatusRechecksAccessAndKeepsHistoryAcrossRegrant(t *testing.T) {
	t.Parallel()
	f, staff, token, _, session := staffHTTPFixture(t)
	path := "/app/massage-staff/" + f.Event.ID.String()
	op := uuid.New()
	args := noticePreference(f.Event.ID, 0, false)
	code, raw := preferenceAgentWrite(t, f, token, op, args)
	saved := preferenceResult(t, code, raw)
	admin := ownerProfileScope(t, f.H, f.AdminID)
	binding := db.MassageBindingChange{
		EventID:                 f.Event.ID,
		SpecialistID:            "alex",
		UserID:                  staff.UserID,
		ExpectedEventVersion:    f.Event.Version,
		ExpectedScheduleVersion: 1,
		ExpectedVersion:         1,
		Active:                  false,
	}
	revoked := bindingChange(t, f, admin, binding)
	code, raw = adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		preferenceAPI+"set_preferences/status",
		map[string]any{"protocol_version": 1, "operation_id": op},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodGet,
		path+"/preference-operations/"+op.String(),
		"",
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusNotFound, code)
	code, raw = preferenceAgentWrite(t, f, token, op, args)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	binding.Active = true
	binding.ExpectedVersion = revoked.Version
	bindingChange(t, f, admin, binding)
	current := massageAppCall[db.MassageSpecialistPreferences](t, f, session, http.MethodGet, path+"/preferences", nil)
	require.Equal(t, saved, current)
	code, raw = adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		preferenceAPI+"set_preferences/status",
		map[string]any{"protocol_version": 1, "operation_id": op},
	)
	require.Equal(t, saved, preferenceResult(t, code, raw))
	_, err := f.H.store.Pool().
		Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
	require.NoError(t, err)
	code, raw = adminRequest(
		t,
		f.H,
		token,
		http.MethodPost,
		preferenceAPI+"set_preferences/status",
		map[string]any{"protocol_version": 1, "operation_id": op},
	)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"not_found"`)
	var version int64
	require.NoError(
		t,
		f.H.store.Pool().
			QueryRow(t.Context(), "SELECT version FROM shuttle_massage_specialist_preferences WHERE user_id=$1", staff.UserID).
			Scan(&version),
	)
	require.EqualValues(t, 1, version)
}

func TestMassageSpecialistPreferenceHTTPRechecksNativeSessionAfterIAMWait(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"session_expired", "binding_revoked", "module_disabled"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, staff, _, _, session := staffHTTPFixture(t)
			p := f.H.peers
			entered, release := make(chan struct{}), make(chan struct{})
			var signal, unblock sync.Once
			t.Cleanup(func() { unblock.Do(func() { close(release) }) })
			p.tokenHook = func(subject string) {
				if subject == staff.Subject {
					signal.Do(func() { close(entered); <-release })
				}
			}
			f.H.iam.Invalidate(staff.Subject)
			body := paymentAppJSON(
				t,
				map[string]any{"operation_id": uuid.New(), "arguments": noticePreference(f.Event.ID, 0, false)},
			)
			type response struct {
				Code int
				Raw  []byte
				Err  error
			}
			done := make(chan response, 1)
			go func() {
				code, raw, _, err := appHTTPCall(
					f.H,
					http.MethodPost,
					"/app/massage-staff/"+f.Event.ID.String()+"/preferences",
					body,
					session.Token,
					appTestOrigin,
				)
				done <- response{Code: code, Raw: raw, Err: err}
			}()
			select {
			case <-entered:
			case <-time.After(8 * time.Second):
				t.Fatal("preference write did not reach IAM wait")
			}
			expected := http.StatusNotFound
			switch change {
			case "session_expired":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_miniapp_sessions SET expires_at=clock_timestamp()-interval '1 "+
						"second' WHERE user_id=$1", staff.UserID)
				require.NoError(t, err)
				expected = http.StatusUnauthorized
			case "binding_revoked":
				bindingChange(
					t,
					f,
					ownerProfileScope(t, f.H, f.AdminID),
					db.MassageBindingChange{
						EventID:                 f.Event.ID,
						SpecialistID:            "alex",
						UserID:                  staff.UserID,
						ExpectedEventVersion:    f.Event.Version,
						ExpectedScheduleVersion: 1,
						ExpectedVersion:         1,
						Active:                  false,
					},
				)
			case "module_disabled":
				_, err := f.H.store.Pool().
					Exec(t.Context(), "UPDATE shuttle_events SET modules=array_remove(modules,'massage') WHERE id=$1", f.Event.ID)
				require.NoError(t, err)
			}
			unblock.Do(func() { close(release) })
			select {
			case result := <-done:
				require.NoError(t, result.Err)
				require.Equal(t, expected, result.Code, string(result.Raw))
			case <-time.After(8 * time.Second):
				t.Fatal("preference write did not finish after IAM release")
			}
			for _, table := range []string{
				"shuttle_massage_specialist_preferences",
				"shuttle_massage_specialist_preference_operations",
			} {
				var count int
				require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM "+table).Scan(&count))
				require.Zero(t, count)
			}
		})
	}
}
