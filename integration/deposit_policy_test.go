package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestDepositPolicyFreezesAtAssignmentAndSurvivesRestart(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9931, true)
	_, user := registrationUser(t, h, 9932, false)
	event := registrationEvent(t, h, operator)
	event.Document.Deposit = &registrations.DepositPolicy{MinimumMinor: 3000, MinimumBPS: 2500}
	path := "/internal/admin/events/" + event.ID.String()
	code, data := adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "deposit-config", "expected_version": event.Version, "document": event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	entry := registerForEvent(t, h, user, event.ID, "deposit-join", "leader")
	body := assignmentBody("deposit-assign", event, entry)
	code, data = adminRequest(t, h, operator, http.MethodPost, assignmentPath(event.ID, entry.ID), body)
	require.Equal(t, http.StatusOK, code, string(data))
	var assigned registrations.Registration
	require.NoError(t, json.Unmarshal(data, &assigned))
	require.EqualValues(t, 3087, assigned.Pass.Deposit.RequiredMinor)
	require.EqualValues(t, 3000, assigned.Pass.Deposit.MinimumMinor)
	require.Equal(t, event.Document.Registration.ClosesAt, assigned.Pass.Deposit.FinalDueAt)
	event.Document.Deposit = &registrations.DepositPolicy{MinimumMinor: 8000, MinimumBPS: 5000}
	event.Document.Registration.PriceMinor = 25000
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "deposit-change", "expected_version": event.Version, "document": event.Document},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &event))
	h.stop(t)
	h.start(t)
	code, data = adminRequest(t, h, user, http.MethodGet, "/internal/events/"+event.ID.String()+"/registration", nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var recovered registrations.Registration
	require.NoError(t, json.Unmarshal(data, &recovered))
	require.Equal(t, assigned, recovered)
	code, data = adminRequest(t, h, operator, http.MethodPost, assignmentPath(event.ID, entry.ID), body)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &recovered))
	require.Equal(t, assigned, recovered)
	_, newUser := registrationUser(t, h, 9933, false)
	newEntry := registerForEvent(t, h, newUser, event.ID, "new-policy-join", "follower")
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPost,
		assignmentPath(event.ID, newEntry.ID),
		assignmentBody("new-policy-assign", event, newEntry),
	)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NoError(t, json.Unmarshal(data, &recovered))
	require.EqualValues(t, 12500, recovered.Pass.Deposit.RequiredMinor)
	require.EqualValues(t, 25000, recovered.Pass.PriceMinor)
}
func TestDepositPolicyRejectsInvalidConfigurationWithoutChangingEvent(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	_, operator := registrationUser(t, h, 9941, true)
	_, user := registrationUser(t, h, 9942, false)
	event := registrationEvent(t, h, operator)
	raw, _ := json.Marshal(event.Document)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	path := "/internal/admin/events/" + event.ID.String()
	for _, policy := range []map[string]any{
		{"minimum_minor": 12346, "minimum_bps": 0}, {"minimum_minor": 0, "minimum_bps": 10001},
		{"minimum_minor": 0},
		{"minimum_minor": nil, "minimum_bps": 0},
		{"minimum_minor": 0, "minimum_bps": 0, "unknown": true},
	} {
		doc["deposit"] = policy
		code, _ := adminRequest(
			t,
			h,
			operator,
			http.MethodPut,
			path,
			map[string]any{"operation_id": "invalid-policy", "expected_version": event.Version, "document": doc},
		)
		require.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, code)
	}
	doc["deposit"] = map[string]any{"minimum_minor": 3000, "minimum_bps": 2500}
	code, _ := adminRequest(
		t,
		h,
		user,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "forbidden-policy", "expected_version": event.Version, "document": doc},
	)
	require.Equal(t, http.StatusForbidden, code)
	code, data := adminRequest(t, h, operator, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code, string(data))
	var unchanged events.Event
	require.NoError(t, json.Unmarshal(data, &unchanged))
	require.Equal(t, event, unchanged)
}
