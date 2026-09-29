package identity_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestIntrospectionFailureIsNotIdentityDenial(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"unavailable", http.StatusServiceUnavailable, "provider secret"},
		{"rate limited", http.StatusTooManyRequests, "provider secret"},
		{"client authentication", http.StatusUnauthorized, "provider secret"},
		{"malformed", http.StatusOK, "{"},
		{"oversized", http.StatusOK, strings.Repeat(" ", 65537)},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			client, err := identity.NewZitadel(zitadelTestConfig(server))
			require.NoError(t, err)
			owner, err := client.Verify(t.Context(), "user-token")
			require.Empty(t, owner)
			require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
			require.NotErrorIs(t, err, identity.ErrZitadelIdentity)
			require.NotContains(t, err.Error(), "secret")
		})
	}
}

func TestIntrospectionContextAndNetworkFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	client, err := identity.NewZitadel(zitadelTestConfig(server))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = client.Verify(ctx, "token")
	require.ErrorIs(t, err, context.Canceled)
	server.Close()
	_, err = client.Verify(t.Context(), "token")
	require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
}
