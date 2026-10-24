package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

func TestRegistrationRulesPersistWithoutRewritingOldEventReceipts(t *testing.T) {
	t.Parallel()
	p := newPeers(t)
	p.loseSend = true
	h := setup(t, p)
	operator := taskAdminToken(t, h, unknownTask(t, h))
	id := uuid.New()
	path := "/internal/admin/events/" + id.String()
	start := time.Date(2026, time.November, 10, 12, 0, 0, 0, time.UTC)
	doc := events.Document{
		Name:     "Registration fixture",
		Timezone: "Europe/Minsk",
		StartsAt: start,
		EndsAt:   start.Add(48 * time.Hour),
		Modules:  []string{"registrations", "orders"},
	}
	original := map[string]any{"operation_id": "legacy-event", "expected_version": 0, "document": doc}
	code, data := adminRequest(t, h, operator, http.MethodPut, path, original)
	require.Equal(t, http.StatusOK, code, string(data))
	require.NotContains(t, string(data), `"registration"`)
	rules := registrations.Rules{
		LeaderCapacity:       2,
		FollowerCapacity:     3,
		PriceMinor:           12345,
		Currency:             " BYN ",
		CurrencyScale:        2,
		ClosesAt:             start.Add(-24 * time.Hour),
		PaymentWindowSeconds: 172800,
	}
	doc.Registration = &rules
	update := map[string]any{"operation_id": "configure-registration", "expected_version": 1, "document": doc}
	code, data = adminRequest(t, h, operator, http.MethodPut, path, update)
	require.Equal(t, http.StatusOK, code, string(data))
	var configured events.Event
	require.NoError(t, json.Unmarshal(data, &configured))
	require.EqualValues(t, 2, configured.Version)
	require.NotNil(t, configured.Document.Registration)
	require.Equal(t, "BYN", configured.Document.Registration.Currency)
	require.EqualValues(t, 12345, configured.Document.Registration.PriceMinor)
	require.Equal(t, " BYN ", rules.Currency)
	h.stop(t)
	h.start(t)
	for _, methodPath := range []string{path, "/internal/admin/event-operations/configure-registration"} {
		code, data = adminRequest(t, h, operator, http.MethodGet, methodPath, nil)
		require.Equal(t, http.StatusOK, code, string(data))
		var read events.Event
		require.NoError(t, json.Unmarshal(data, &read))
		require.Equal(t, configured, read)
	}
	code, data = adminRequest(t, h, operator, http.MethodPut, path, update)
	require.Equal(t, http.StatusOK, code, string(data))
	var replay events.Event
	replay = events.Event{}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, configured, replay)
	code, data = adminRequest(t, h, operator, http.MethodPut, path, original)
	require.Equal(t, http.StatusOK, code, string(data))
	replay = events.Event{}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.EqualValues(t, 1, replay.Version)
	require.Nil(t, replay.Document.Registration)
	for _, change := range []struct {
		name  string
		apply func(*registrations.Rules)
	}{
		{"negative-capacity", func(r *registrations.Rules) { r.LeaderCapacity = -1 }},
		{"zero-window", func(r *registrations.Rules) { r.PaymentWindowSeconds = 0 }},
		{"late-close", func(r *registrations.Rules) { r.ClosesAt = doc.EndsAt.Add(time.Second) }},
		{"invalid-currency", func(r *registrations.Rules) { r.Currency = "byn" }},
		{"unsafe-json-price", func(r *registrations.Rules) { r.PriceMinor = 1 << 53 }},
	} {
		invalid := rules
		change.apply(&invalid)
		candidate := doc
		candidate.Registration = &invalid
		code, data = adminRequest(
			t,
			h,
			operator,
			http.MethodPut,
			path,
			map[string]any{"operation_id": change.name, "expected_version": 2, "document": candidate},
		)
		require.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, code, string(data))
	}
	code, data = adminRequest(t, h, operator, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, code, string(data))
	replay = events.Event{}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, configured, replay)
	// Missing or null zero-valid fields must not silently become free/zero-capacity.
	validJSON, err := json.Marshal(configured.Document.Registration)
	require.NoError(t, err)
	for _, field := range []string{"price_minor", "leader_capacity", "follower_capacity", "currency_scale"} {
		for _, nullValue := range []bool{false, true} {
			var broken map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(validJSON, &broken))
			if nullValue {
				broken[field] = json.RawMessage("null")
			} else {
				delete(broken, field)
			}
			brokenJSON, marshalErr := json.Marshal(broken)
			require.NoError(t, marshalErr)
			candidate := map[string]any{
				"name":         doc.Name,
				"timezone":     doc.Timezone,
				"starts_at":    doc.StartsAt,
				"ends_at":      doc.EndsAt,
				"modules":      doc.Modules,
				"registration": json.RawMessage(brokenJSON),
			}
			code, data = adminRequest(
				t,
				h,
				operator,
				http.MethodPut,
				path,
				map[string]any{"operation_id": "invalid-presence", "expected_version": 2, "document": candidate},
			)
			require.Contains(t, []int{http.StatusBadRequest, http.StatusUnprocessableEntity}, code, string(data))
			_, updateErr := h.store.Pool().
				Exec(context.Background(), "UPDATE shuttle_event_registration_rules SET document=$2 WHERE event_id=$" +
"1", id, brokenJSON)
			require.NoError(t, updateErr)
			code, _ = adminRequest(t, h, operator, http.MethodGet, path, nil)
			require.Equal(t, http.StatusServiceUnavailable, code)
		}
	}
	_, restoreErr := h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_event_registration_rules SET document=$2 WHERE event_id=$1", id, validJSON)
	require.NoError(t, restoreErr)
	// Stored JSON can be syntactically valid but invalid business configuration.
	_, err = h.store.Pool().
		Exec(context.Background(), "UPDATE shuttle_event_registration_rules SET document='{}' WHERE event_id=$1", id)
	require.NoError(t, err)
	code, _ = adminRequest(t, h, operator, http.MethodGet, path, nil)
	require.Equal(t, http.StatusServiceUnavailable, code)
	doc.Registration = nil
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodPut,
		path,
		map[string]any{"operation_id": "clear-registration", "expected_version": 2, "document": doc},
	)
	require.Equal(t, http.StatusOK, code, string(data))
	replay = events.Event{}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.EqualValues(t, 3, replay.Version)
	require.Nil(t, replay.Document.Registration)
	code, data = adminRequest(
		t,
		h,
		operator,
		http.MethodGet,
		"/internal/admin/event-operations/configure-registration",
		nil,
	)
	require.Equal(t, http.StatusOK, code, string(data))
	replay = events.Event{}
	require.NoError(t, json.Unmarshal(data, &replay))
	require.Equal(t, configured, replay)
}
