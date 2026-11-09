package loom_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

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

func TestStartupReadinessUsesOnlyPublicEndpoint(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		status int
		body   string
		ready  bool
	}{
		{"ready", http.StatusOK, `{"status":"ready"}`, true},
		{"unavailable", http.StatusServiceUnavailable, "PRIVATE_UPSTREAM", false},
		{"wrong status", http.StatusOK, `{"status":"starting"}`, false},
		{"missing status", http.StatusOK, `{}`, false},
		{"malformed", http.StatusOK, "PRIVATE_UPSTREAM", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, http.MethodGet, r.Method)
				assert.Equal(t, "/readyz", r.URL.RequestURI())
				assert.Empty(t, r.Header.Get("Authorization"))
				assert.Empty(t, r.Header.Get("X-Subject-Token"))
				assert.Empty(t, r.Header.Get("X-Auth-Session-Id"))
				w.WriteHeader(test.status)
				_, _ = fmt.Fprint(w, test.body)
			}))
			t.Cleanup(server.Close)
			client, err := loom.New(server.URL, server.Client())
			require.NoError(t, err)
			err = client.CheckReady(t.Context())
			if test.ready {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.NotContains(t, err.Error(), "PRIVATE_UPSTREAM")
			}
		})
	}
}

func TestStartupReadinessRejectsRedirectWithoutFollowing(t *testing.T) {
	t.Parallel()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("readiness followed redirect")
		_, _ = fmt.Fprint(w, `{"status":"ready"}`)
	}))
	t.Cleanup(target.Close)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/private-token", http.StatusFound)
	}))
	t.Cleanup(server.Close)
	client, err := loom.New(server.URL, server.Client())
	require.NoError(t, err)
	err = client.CheckReady(t.Context())
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private-token")
}

type readinessTimeoutTransport struct{}

func (readinessTimeoutTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	<-r.Context().Done()
	return nil, r.Context().Err()
}

func TestStartupReadinessTimeoutKeepsEndpointPrivate(t *testing.T) {
	t.Parallel()
	client, err := loom.New("https://private-token.invalid", &http.Client{
		Timeout: 10 * time.Millisecond, Transport: readinessTimeoutTransport{},
	})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	err = client.CheckReady(ctx)
	require.Error(t, err)
	require.NoError(t, ctx.Err(), "the HTTP timeout must bound the readiness probe")
	require.NotContains(t, err.Error(), "private-token")
}
