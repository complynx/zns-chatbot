package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/identity"
	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestIdentityMetricsAreLocalAndPrivate(t *testing.T) {
	t.Parallel()
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	client, err := identity.New(identity.Options{
		Issuer: server.URL, OrganizationID: "private-org", ActorID: "private-actor",
		BotID: 123, LoomAudience: "private-loom", APIAudience: "private-api",
		ActorClientCredentials:         identity.ClientCredentials{ID: "private-client", Secret: "private-secret"},
		ExchangeClientCredentials:      identity.ClientCredentials{ID: "private-client", Secret: "private-secret"},
		IntrospectionClientCredentials: identity.ClientCredentials{ID: "private-client", Secret: "private-secret"},
	}, server.Client())
	require.NoError(t, err)
	telemetry, err := observability.New(context.Background(), observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(context.Background())) })
	require.NoError(t, telemetry.ObserveIdentity(client))
	for range 2 {
		response := httptest.NewRecorder()
		telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, response.Code)
		body := response.Body.String()
		for _, series := range []string{
			`zinusya_identity_positive_cache_lookups_total{outcome="hit"} 0`,
			`zinusya_identity_positive_cache_lookups_total{outcome="miss"} 0`,
			`zinusya_identity_token_fallbacks_total{kind="service"} 0`,
			`zinusya_identity_token_fallbacks_total{kind="user"} 0`,
		} {
			require.Contains(t, body, series)
		}
		for _, private := range []string{"private-org", "private-actor", "private-client", "private-secret", server.URL} {
			require.NotContains(t, body, private)
		}
	}
	require.Zero(t, calls.Load(), "scraping must not contact IAM")
	require.Equal(t, identity.CacheStats{}, client.CacheStats())
}
