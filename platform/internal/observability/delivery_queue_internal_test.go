package observability

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
)

func TestDeliveryQueueCollectorFailureDoesNotReportEmptyOrStaleQueue(t *testing.T) {
	t.Parallel()
	item := delivery.QueueObservation{
		Owner:              delivery.Orders,
		Class:              delivery.Interactive,
		State:              delivery.Deferred,
		Count:              3,
		UnknownAge:         1,
		OldestAgeSeconds:   120,
		Delayed:            2,
		NextAttemptSeconds: 30,
		MaximumWaitSeconds: 50,
	}
	items := []delivery.QueueObservation{item}
	var readErr error
	registry := prometheus.NewRegistry()
	require.NoError(
		t,
		registry.Register(
			newDeliveryQueueCollector(
				func(context.Context) ([]delivery.QueueObservation, error) { return items, readErr },
			),
		),
	)
	scrape := func() string {
		response := httptest.NewRecorder()
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
			ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, response.Code)
		return response.Body.String()
	}
	text := scrape()
	require.Contains(t, text, `zns_delivery_snapshot_available 1`)
	require.Contains(t, text, `zns_delivery_queue_backlog{class="interactive",owner="orders",state="pending"} 3`)
	require.Contains(t, text, `zns_delivery_queue_age_unknown{class="interactive",owner="orders",state="pending"} 1`)
	readErr = errors.New("postgres://private-user:private-password@database/private-query")
	text = scrape()
	require.Contains(t, text, `zns_delivery_snapshot_available 0`)
	require.NotContains(t, text, "zns_delivery_queue_backlog")
	require.NotContains(t, text, "private")
	readErr = nil
	items = nil
	text = scrape()
	require.Contains(t, text, `zns_delivery_snapshot_available 1`)
	require.NotContains(t, text, "zns_delivery_queue_backlog")
}

func TestDeliveryQueueCollectorRejectsUnboundedOrPrivateDimensions(t *testing.T) {
	t.Parallel()
	item := delivery.QueueObservation{
		Owner: delivery.Orders,
		Class: delivery.Interactive,
		State: delivery.Deferred,
		Count: 1,
	}
	for _, items := range [][]delivery.QueueObservation{
		{{Owner: "private-owner", Class: delivery.Interactive, State: delivery.Deferred}},
		{item, item}, make([]delivery.QueueObservation, 71),
	} {
		registry := prometheus.NewRegistry()
		require.NoError(
			t,
			registry.Register(
				newDeliveryQueueCollector(
					func(context.Context) ([]delivery.QueueObservation, error) { return items, nil },
				),
			),
		)
		families, err := registry.Gather()
		require.NoError(t, err)
		require.Len(t, families, 1)
		require.Equal(t, "zns_delivery_snapshot_available", families[0].GetName())
		require.Zero(t, families[0].GetMetric()[0].GetGauge().GetValue())
	}
}
