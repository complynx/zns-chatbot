package integration_test

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/runtimeapp"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func TestPassAdminAPIAuthenticatesActor(t *testing.T) {
	t.Parallel()
	db, service := bookingFixture(t)
	alice, err := service.Execute(t.Context(), "alice", bookingCommand("solo", "register", passbooking.Booking{}))
	require.NoError(t, err)
	signer := identity.Signer{Key: []byte(strings.Repeat("p", 32))}
	handler := api.Handler(runtimeapp.NewServices(db, runtimeapp.Options{}), signer, slog.New(slog.DiscardHandler))
	price := 0
	command := passbooking.AdminAssignment{
		Event:         "dance",
		Key:           "admin-free",
		Target:        "alice",
		TargetVersion: alice.Version,
		TotalPrice:    &price,
	}
	body, err := json.Marshal(command)
	require.NoError(t, err)
	post := func(actor, encoded string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(http.MethodPost, "/v1/passes/admin/assign", strings.NewReader(encoded))
		if actor != "" {
			request.Header.Set("Authorization", "Bearer "+signer.Token(actor))
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		return response
	}
	assert.Equal(t, http.StatusUnauthorized, post("", string(body)).Code)
	assert.Equal(t, http.StatusForbidden, post("alice", string(body)).Code)
	assert.Equal(t, http.StatusBadRequest, post("bob", `{"actor":"alice"}`).Code)
	response := post("bob", string(body))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result passbooking.AdminAssignmentResult
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Len(t, result.Bookings, 1)
	assert.Equal(t, "paid", result.Bookings[0].State)
	require.NotNil(t, result.Bookings[0].Price)
	assert.Zero(t, *result.Bookings[0].Price)
	assert.Equal(t, http.StatusOK, post("bob", string(body)).Code, "same operation replay")
}
