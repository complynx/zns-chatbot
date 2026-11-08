//nolint:testpackage // Manual collection proves scrape callbacks only read snapshots.
package observability

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/complynx/zns-chatbot/internal/loom"
)

func TestDescriptorBillingManualSnapshotFailureAndRecovery(t *testing.T) {
	t.Parallel()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { require.NoError(t, provider.Shutdown(t.Context())) })
	telemetry := &Telemetry{meters: provider}
	ok, at := false, time.Time{}
	require.NoError(t, telemetry.ObserveDescriptorBilling(func() (loom.CreditBalance, bool, time.Time) {
		return loom.CreditBalance{AvailableMicrocredits: 5000000, ReservedMicrocredits: 100}, ok, at
	}))
	collect := func() map[string]float64 {
		t.Helper()
		var data metricdata.ResourceMetrics
		require.NoError(t, reader.Collect(t.Context(), &data))
		values := map[string]float64{}
		for _, scope := range data.ScopeMetrics {
			for _, item := range scope.Metrics {
				switch gauge := item.Data.(type) {
				case metricdata.Gauge[int64]:
					values[item.Name] = float64(gauge.DataPoints[0].Value)
					require.Zero(t, gauge.DataPoints[0].Attributes.Len())
				case metricdata.Gauge[float64]:
					values[item.Name] = gauge.DataPoints[0].Value
				}
			}
		}
		return values
	}
	values := collect()
	require.Zero(t, values["zinusya.descriptor.billing.collection_success"])
	require.Zero(t, values["zinusya.descriptor.billing.last_success_timestamp"])
	require.NotContains(t, values, "zinusya.descriptor.billing.available_microcredits")
	ok, at = true, time.Unix(1000, 0)
	values = collect()
	require.InDelta(t, 5000000, values["zinusya.descriptor.billing.available_microcredits"], 0)
	require.InDelta(t, 100, values["zinusya.descriptor.billing.reserved_microcredits"], 0)
	ok = false
	values = collect()
	require.NotContains(t, values, "zinusya.descriptor.billing.available_microcredits")
	require.InDelta(t, 1000, values["zinusya.descriptor.billing.last_success_timestamp"], 0)
	ok, at = true, time.Unix(1060, 0)
	values = collect()
	require.InDelta(t, 1, values["zinusya.descriptor.billing.collection_success"], 0)
	require.InDelta(t, 1060, values["zinusya.descriptor.billing.last_success_timestamp"], 0)
}

func TestDescriptorBillingPrometheusNames(t *testing.T) {
	t.Parallel()
	telemetry, err := New(t.Context(), Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(t.Context())) })
	require.NoError(t, telemetry.ObserveDescriptorBilling(func() (loom.CreditBalance, bool, time.Time) {
		return loom.CreditBalance{AvailableMicrocredits: 5000000, ReservedMicrocredits: 100}, true, time.Unix(1000, 0)
	}))
	response := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, line := range []string{
		"zinusya_descriptor_billing_available_microcredits 5e+06",
		"zinusya_descriptor_billing_reserved_microcredits 100",
		"zinusya_descriptor_billing_collection_success 1",
		"zinusya_descriptor_billing_last_success_timestamp_seconds 1000",
	} {
		require.Contains(t, response.Body.String(), line)
	}
}
