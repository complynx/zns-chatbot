package identity_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestOnlyExplicitUserExchangeInactivityIsTerminal(t *testing.T) {
	t.Parallel()
	const inactive = `{"error":"invalid_request","error_description":"Errors.User.NotActive"}`
	for _, test := range []struct {
		name   string
		status int
		body   string
		actor  bool
		denied bool
	}{
		{"inactive user", http.StatusBadRequest, inactive, false, true},
		{"inactive actor", http.StatusBadRequest, inactive, true, false},
		{"unauthorized", http.StatusUnauthorized, inactive, false, false},
		{"provider unavailable", http.StatusServiceUnavailable, inactive, false, false},
		{"client credentials", http.StatusBadRequest, `{"error":"invalid_client"}`, false, false},
		{"unknown rejection", http.StatusBadRequest, `{"error":"invalid_request","error_description":"Errors.PermissionDenied"}`, false, false},
		{"wrong OAuth error", http.StatusBadRequest, `{"error":"invalid_grant","error_description":"Errors.User.NotActive"}`, false, false},
		{"malformed", http.StatusBadRequest, `{`, false, false},
		{"oversized", http.StatusBadRequest, inactive + strings.Repeat(" ", 4096), false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			t.Cleanup(server.Close)
			config := zitadelTestConfig(server)
			if test.actor {
				config.ActorToken = ""
				config.ActorClientID, config.ActorClientSecret = "actor-client", "actor-secret"
			}
			client, err := identity.NewZitadel(config)
			require.NoError(t, err)
			token, err := client.Exchange(t.Context(), "mapped-user")
			require.Empty(t, token)
			if test.denied {
				require.ErrorIs(t, err, identity.ErrZitadelUserInactive)
			} else {
				require.ErrorIs(t, err, identity.ErrZitadelUnavailable)
				require.NotErrorIs(t, err, identity.ErrZitadelUserInactive)
			}
		})
	}
}

func TestProviderConnectionFailureIsNotTerminal(t *testing.T) {
	t.Parallel()
	server := httptest.NewTLSServer(http.NotFoundHandler())
	client, err := identity.NewZitadel(zitadelTestConfig(server))
	require.NoError(t, err)
	server.Close()
	_, err = client.Exchange(t.Context(), "mapped-user")
	require.Error(t, err)
	require.NotErrorIs(t, err, identity.ErrZitadelUserInactive)
}
