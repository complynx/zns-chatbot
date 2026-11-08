package loom_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestOwnCreditsUsesOnlyOwnMachineAndKeepsAvailableNetOfReservations(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"available_microcredits":750000,"reserved_microcredits":250000,"monthly_funding":{}}`,
		`{"available_microcredits":-1,"reserved_microcredits":0}`,
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/users/me/credits", r.URL.RequestURI())
			assert.Equal(t, "Bearer machine-only", r.Header.Get("Authorization"))
			assert.Empty(t, r.Header.Get("X-Subject-Token"))
			assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
			_, _ = fmt.Fprint(w, body)
		}))
		client, err := loom.New(server.URL, server.Client())
		require.NoError(t, err)
		balance, readErr := client.OwnCredits(t.Context(), "machine-only")
		server.Close()
		require.NoError(t, readErr)
		if balance.ReservedMicrocredits > 0 {
			require.EqualValues(t, 750000, balance.AvailableMicrocredits)
		} else {
			require.EqualValues(t, -1, balance.AvailableMicrocredits)
		}
	}
}

func TestOwnCreditsRejectsUnavailableAndMalformedWithoutLeakingReply(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{}`, `{"available_microcredits":0}`, `{"available_microcredits":0,"reserved_microcredits":-1}`, "PRIVATE_BODY",
	} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = fmt.Fprint(w, body)
		}))
		client, err := loom.New(server.URL, server.Client())
		require.NoError(t, err)
		_, readErr := client.OwnCredits(t.Context(), "private-token")
		server.Close()
		require.Error(t, readErr)
		require.NotContains(t, readErr.Error(), "PRIVATE_BODY")
		require.NotContains(t, readErr.Error(), "private-token")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "PRIVATE_FAILURE", http.StatusForbidden)
	}))
	defer server.Close()
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	_, err = client.OwnCredits(t.Context(), "private-token")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "PRIVATE_FAILURE")
}
