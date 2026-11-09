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

func TestStartupModelsOnlyReadsServiceCatalog(t *testing.T) {
	t.Parallel()
	for _, body := range []string{`{"models":[]}`, `{"models":[{"id":"example"}]}`, `{}`, `{"models":null}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Equal(t, "/v1/models", r.URL.RequestURI())
			assert.Equal(t, "Bearer machine-only", r.Header.Get("Authorization"))
			assert.Empty(t, r.Header.Get("X-Subject-Token"))
			assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
			assert.Empty(t, r.Header.Get("Idempotency-Key"))
			_, _ = fmt.Fprint(w, body)
		}))
		client, err := loom.New(server.URL, server.Client())
		require.NoError(t, err)
		err = client.CheckModels(t.Context(), "machine-only")
		server.Close()
		if body == `{}` || body == `{"models":null}` {
			require.Error(t, err)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestStartupModelsFailureKeepsCredentialsPrivate(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "PRIVATE_UPSTREAM", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	err = client.CheckModels(t.Context(), "private-token")
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-token")
	require.NotContains(t, err.Error(), "PRIVATE_UPSTREAM")
}
