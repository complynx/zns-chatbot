package observability_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestResponseLatencyOmitsClockAnomaliesAndExportsUnlabelledSeconds(t *testing.T) {
	t.Parallel()
	telemetry, err := observability.New(t.Context(), observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(context.Background())) })
	for _, seconds := range []float64{-1, math.NaN(), math.Inf(1), math.Inf(-1)} {
		telemetry.RecordResponseLatency(t.Context(), seconds)
	}
	read := func() string {
		w := httptest.NewRecorder()
		telemetry.MetricsHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, w.Code)
		return w.Body.String()
	}
	require.NotContains(t, read(), "zinusya_response_confirmed_duration_seconds_count")
	telemetry.RecordResponseLatency(t.Context(), 2)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	telemetry.RecordResponseLatency(ctx, 7200)
	body := read()
	require.Contains(t, body, "\nzinusya_response_confirmed_duration_seconds_count 2\n")
	require.Contains(t, body, "\nzinusya_response_confirmed_duration_seconds_sum 7202\n")
	require.Contains(t, body, "zinusya_response_confirmed_duration_seconds_bucket{le=\"3600\"} 1\n")
	require.Contains(t, body, "zinusya_response_confirmed_duration_seconds_bucket{le=\"21600\"} 2\n")
	require.NotContains(t, body, "zinusya_response_confirmed_duration_seconds_count{")
}
