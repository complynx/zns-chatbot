package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type specialistSoonPreferences struct {
	UserID         uuid.UUID `json:"user_id"`
	NotifyBookings bool      `json:"notify_bookings"`
	NotifyNext     bool      `json:"notify_next"`
	Version        int64     `json:"version"`
}

func TestMassageSpecialistSoonPreferencesIndependentAcrossAgentAndNative(t *testing.T) {
	t.Parallel()
	f, staff, token, _, session := staffHTTPFixture(t)
	path := "/app/massage-staff/" + f.Event.ID.String()
	initial := massageAppCall[specialistSoonPreferences](t, f, session, http.MethodGet, path+"/preferences", nil)
	require.Equal(t, specialistSoonPreferences{UserID: staff.UserID, NotifyBookings: true, NotifyNext: true}, initial)
	setAgent := func(operation uuid.UUID, args map[string]any) specialistSoonPreferences {
		t.Helper()
		code, raw := preferenceAgentWrite(t, f, token, operation, args)
		require.Equal(t, http.StatusOK, code, string(raw))
		var reply struct {
			Status string                    `json:"status"`
			Result specialistSoonPreferences `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &reply))
		require.Equal(t, "succeeded", reply.Status, string(raw))
		return reply.Result
	}
	op := uuid.New()
	args := map[string]any{"event_id": f.Event.ID, "expected_version": 0, "notify_next": false}
	disabled := setAgent(op, args)
	require.Equal(
		t,
		specialistSoonPreferences{UserID: staff.UserID, NotifyBookings: true, NotifyNext: false, Version: 1},
		disabled,
	)
	native := massageAppCall[specialistSoonPreferences](
		t,
		f,
		session,
		http.MethodPost,
		path+"/preferences",
		map[string]any{
			"operation_id": uuid.New(),
			"arguments":    map[string]any{"event_id": f.Event.ID, "expected_version": 1, "notify_bookings": false},
		},
	)
	require.Equal(
		t,
		specialistSoonPreferences{UserID: staff.UserID, NotifyBookings: false, NotifyNext: false, Version: 2},
		native,
	)
	stale := map[string]any{"event_id": f.Event.ID, "expected_version": 1, "notify_next": true}
	code, raw := preferenceAgentWrite(t, f, token, uuid.New(), stale)
	require.Equal(t, http.StatusOK, code, string(raw))
	require.Contains(t, string(raw), `"status":"rejected"`)
	code, _, _ = appCall(
		t,
		f.H,
		http.MethodPost,
		path+"/preferences",
		paymentAppJSON(t, map[string]any{"operation_id": uuid.New(), "arguments": stale}),
		session.Token,
		appTestOrigin,
	)
	require.Equal(t, http.StatusConflict, code)
	unchanged := massageAppCall[specialistSoonPreferences](t, f, session, http.MethodGet, path+"/preferences", nil)
	require.Equal(t, native, unchanged, "Stale new operations cannot change either preference")
	current := setAgent(uuid.New(), map[string]any{"event_id": f.Event.ID, "expected_version": 2, "notify_next": true})
	require.Equal(
		t,
		specialistSoonPreferences{UserID: staff.UserID, NotifyBookings: false, NotifyNext: true, Version: 3},
		current,
	)
	changes := []struct {
		api      string
		field    string
		value    bool
		bookings bool
		next     bool
	}{
		{"native", "notify_next", false, false, false},
		{"agent", "notify_bookings", true, true, false},
		{"agent", "notify_bookings", false, false, false},
		{"native", "notify_next", true, false, true},
		{"native", "notify_bookings", true, true, true},
		{"native", "notify_next", false, true, false},
	}
	for _, change := range changes {
		changeArgs := map[string]any{
			"event_id":         f.Event.ID,
			"expected_version": current.Version,
			change.field:       change.value,
		}
		version := current.Version + 1
		if change.api == "agent" {
			current = setAgent(uuid.New(), changeArgs)
		} else {
			current = massageAppCall[specialistSoonPreferences](
				t,
				f,
				session,
				http.MethodPost,
				path+"/preferences",
				map[string]any{"operation_id": uuid.New(), "arguments": changeArgs},
			)
		}
		require.Equal(
			t,
			specialistSoonPreferences{
				UserID:         staff.UserID,
				NotifyBookings: change.bookings,
				NotifyNext:     change.next,
				Version:        version,
			},
			current,
			change.api+" "+change.field,
		)
	}
	require.Equal(t, disabled, setAgent(op, args), "Original replay cannot overwrite a newer independent preference")
	receipt := massageAppCall[specialistSoonPreferences](
		t,
		f,
		session,
		http.MethodGet,
		path+"/preference-operations/"+op.String(),
		nil,
	)
	require.Equal(t, disabled, receipt)
	after := massageAppCall[specialistSoonPreferences](t, f, session, http.MethodGet, path+"/preferences", nil)
	require.Equal(t, current, after)
	var tasks int
	require.NoError(t, f.H.store.Pool().QueryRow(t.Context(), "SELECT count(*) FROM shuttle_tasks").Scan(&tasks))
	require.Zero(t, tasks, "Settings through either API do not start an agent task")
}
