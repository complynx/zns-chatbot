package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestIdentityCacheCollectorFailureOmitsPriorSnapshot(t *testing.T) {
	t.Parallel()
	var fail atomic.Bool
	type deadlineEvidence struct {
		present   bool
		remaining time.Duration
	}
	evidence := make(chan deadlineEvidence, 1)
	registry := prometheus.NewRegistry()
	require.NoError(
		t,
		registry.Register(
			newIdentityCacheCollector(IdentityCacheAPI, func(ctx context.Context) (identity.CacheObservations, error) {
				deadline, ok := ctx.Deadline()
				// Gather invokes this reader in a worker; capture evidence without fatal
				// assertions or a blocking handoff, then assert after Gather in the test.
				select {
				case evidence <- deadlineEvidence{present: ok, remaining: time.Until(deadline)}:
				default:
					return identity.CacheObservations{}, errors.New("duplicate deadline evidence")
				}
				if fail.Load() {
					return identity.CacheObservations{}, errors.New("private-token private-subject")
				}
				return identity.CacheObservations{
					Exchange: identity.CacheObservation{Usable: 3, Expired: 2, Flights: 1},
					Capacity: 1024,
				}, nil
			}),
		),
	)
	scrape := func() string {
		response := httptest.NewRecorder()
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
			ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, 200, response.Code)
		select {
		case observed := <-evidence:
			require.True(t, observed.present)
			require.LessOrEqual(t, observed.remaining, 2*time.Second)
		default:
			t.Fatal("collector deadline evidence missing")
		}
		return response.Body.String()
	}
	text := scrape()
	assert.Contains(t, text, `zns_identity_cache_entries{adapter="api",cache="exchange",state="usable"} 3`)
	assert.Contains(t, text, `zns_identity_cache_entries{adapter="api",cache="exchange",state="expired"} 2`)
	assert.Contains(t, text, `zns_identity_cache_flights{adapter="api",cache="introspection"} 0`)
	fail.Store(true)
	text = scrape()
	assert.Contains(t, text, `zns_identity_cache_snapshot_available{adapter="api"} 0`)
	assert.NotContains(t, text, "zns_identity_cache_entries")
	assert.NotContains(t, text, "private")
	fail.Store(false)
	assert.Contains(t, scrape(), `zns_identity_cache_snapshot_available{adapter="api"} 1`)
}

func TestIdentityCacheRegistrationRolesAndBounds(t *testing.T) {
	t.Parallel()
	runtime, err := New(t.Context(), Config{})
	require.NoError(t, err)
	require.Error(t, runtime.RegisterIdentityCaches(IdentityCacheRole("subject-secret"), &identity.Zitadel{}))
	require.Error(t, runtime.RegisterIdentityCaches(IdentityCacheAPI, nil))
	require.NoError(t, runtime.RegisterIdentityCaches(IdentityCacheAPI, &identity.Zitadel{}))
	require.NoError(t, runtime.RegisterIdentityCaches(IdentityCacheBot, &identity.Zitadel{}))
	require.Error(t, runtime.RegisterIdentityCaches(IdentityCacheAPI, &identity.Zitadel{}))
	families, err := runtime.Registry.Gather()
	require.NoError(t, err)
	series := 0
	for _, family := range families {
		series += len(family.GetMetric())
	}
	require.Equal(t, 18, series, "two roles each expose exactly nine bounded series")
	for _, item := range []identity.CacheObservations{
		{}, {Capacity: 1024, Exchange: identity.CacheObservation{Expired: -1}},
		{Capacity: 1024, Exchange: identity.CacheObservation{Usable: 1024, Flights: 1}},
	} {
		registry := prometheus.NewRegistry()
		require.NoError(
			t,
			registry.Register(
				newIdentityCacheCollector(
					IdentityCacheAPI,
					func(context.Context) (identity.CacheObservations, error) { return item, nil },
				),
			),
		)
		values, gatherErr := registry.Gather()
		require.NoError(t, gatherErr)
		require.Len(t, values, 1)
		require.Zero(t, values[0].GetMetric()[0].GetGauge().GetValue())
	}
}
