package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/appservices"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func massageHTTP(
	t *testing.T,
	server *httptest.Server,
	signer identity.Signer,
	actor, method, path string,
	body any,
) (int, []byte) {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, bytes.NewReader(encoded))
	require.NoError(t, err)
	if actor != "" {
		request.Header.Set("Authorization", "Bearer "+signer.Token(actor))
	}
	response, err := server.Client().Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, raw
}

func TestMassageAuthenticatedAPI(t *testing.T) {
	t.Parallel()
	db, _, _ := massageFixture(t)
	signer := identity.Signer{Key: []byte(strings.Repeat("m", 32))}
	server := httptest.NewServer(
		api.Handler(appservices.NewServices(db, appservices.Options{}), signer, slog.New(slog.DiscardHandler)),
	)
	defer server.Close()
	const event = "?event=sandbox-festival"
	status, _ := massageHTTP(t, server, signer, "", http.MethodGet, "/v1/massage/parties"+event, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, _ = massageHTTP(t, server, signer, "", http.MethodGet, "/v1/massage/provider-names"+event, nil)
	assert.Equal(t, http.StatusUnauthorized, status)
	status, namesBody := massageHTTP(
		t,
		server,
		signer,
		"alice",
		http.MethodGet,
		"/v1/massage/provider-names"+event,
		nil,
	)
	require.Equal(t, http.StatusOK, status, string(namesBody))
	assert.JSONEq(t, `{"bob":"Bob","master":"Master"}`, string(namesBody))
	status, namesBody = massageHTTP(
		t,
		server,
		signer,
		"alice",
		http.MethodGet,
		"/v1/massage/provider-names?event=unknown",
		nil,
	)
	require.Equal(t, http.StatusOK, status, string(namesBody))
	assert.JSONEq(t, `{}`, string(namesBody))
	status, body := massageHTTP(
		t,
		server,
		signer,
		"alice",
		http.MethodGet,
		"/v1/massage/slots"+event+"&party=night&length=2",
		nil,
	)
	require.Equal(t, http.StatusOK, status, string(body))
	var availability massage.Availability
	require.NoError(t, json.Unmarshal(body, &availability))
	require.NotEmpty(t, availability.Slots)
	assert.NotContains(t, string(body), "notify_bookings")
	assert.NotContains(t, string(body), "notify_next")
	assert.NotContains(t, string(body), "legacy_table_flag")
	assert.NotContains(t, string(body), "work")
	require.NotEmpty(t, availability.Providers)
	assert.Positive(t, availability.Providers[0].ID, "public specialist contact ID supports person links")
	status, body = massageHTTP(
		t,
		server,
		signer,
		"alice",
		http.MethodPost,
		"/v1/massage/actions",
		massageBook("http", "bob", 2, 1),
	)
	require.Equal(t, http.StatusOK, status, string(body))
	var booking massage.Reservation
	require.NoError(t, json.Unmarshal(body, &booking))
	status, body = massageHTTP(t, server, signer, "visitor", http.MethodGet, "/v1/massage/bookings"+event, nil)
	assert.Equal(t, http.StatusOK, status)
	assert.NotContains(t, string(body), booking.ID)
	status, _ = massageHTTP(t, server, signer, "visitor", http.MethodGet, "/v1/massage/timetable"+event, nil)
	assert.Equal(t, http.StatusForbidden, status)
	status, body = massageHTTP(t, server, signer, "bob", http.MethodGet, "/v1/massage/timetable"+event, nil)
	require.Equal(t, http.StatusOK, status, string(body))
	assert.Contains(t, string(body), booking.ID)
	assert.Contains(t, string(body), "Алиса")
	status, _ = massageHTTP(
		t,
		server,
		signer,
		"alice",
		http.MethodPost,
		"/v1/massage/actions",
		map[string]any{"key": "forged", "event": "sandbox-festival", "action": "book", "owner": "bob"},
	)
	assert.Equal(t, http.StatusBadRequest, status)
	status, _ = massageHTTP(
		t,
		server,
		signer,
		"alice",
		http.MethodPut,
		"/v1/massage/preferences"+event,
		massage.Preferences{},
	)
	assert.Equal(t, http.StatusForbidden, status)
	status, _ = massageHTTP(
		t,
		server,
		signer,
		"bob",
		http.MethodPut,
		"/v1/massage/preferences"+event,
		massage.Preferences{},
	)
	assert.Equal(t, http.StatusOK, status)
	status, _ = massageHTTP(t, server, signer, "alice", http.MethodPost, "/v1/massage/actions", massage.Command{
		Key: "http-cancel", Action: "cancel", Event: booking.Event, Booking: booking.ID, Version: booking.Version})
	assert.Equal(t, http.StatusOK, status)
}
