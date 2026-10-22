package observability_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"

	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestWorkerDurationMetricsBoundLabelsAndRecordSecondsWithCancelledContext(t *testing.T) {
	t.Parallel()
	globalTrace, globalMeter := otel.GetTracerProvider(), otel.GetMeterProvider()
	telemetry, err := observability.New(t.Context(), observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(context.Background())) })
	for _, kind := range []string{"shuttle_task", "zinusya_profile_name_sync", "zinusya_payment_notification", "zinusya_pair_notification", "zinusya_order_notification", "zinusya_knowledge_notification", "zinusya_avatar_delivery", "zinusya_massage_reminder", "zinusya_massage_specialist_notice"} {
		for _, outcome := range []string{"returned", "snoozed", "cancelled", "error"} {
			telemetry.RecordWorkerDuration(t.Context(), kind, outcome, 250*time.Millisecond)
		}
	}
	for i := range 100 {
		telemetry.RecordWorkerDuration(
			t.Context(),
			fmt.Sprintf("private-kind-%d", i),
			fmt.Sprintf("private-error-%d", i),
			time.Millisecond,
		)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	telemetry.RecordWorkerDuration(ctx, "shuttle_task", "error", 250*time.Millisecond)
	w := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	for _, kind := range []string{"shuttle_task", "zinusya_profile_name_sync", "zinusya_payment_notification", "zinusya_pair_notification", "zinusya_order_notification", "zinusya_knowledge_notification", "zinusya_avatar_delivery", "zinusya_massage_reminder", "zinusya_massage_specialist_notice"} {
		for _, outcome := range []string{"returned", "snoozed", "cancelled", "error"} {
			count, sum := 1, "0.25"
			if kind == "shuttle_task" && outcome == "error" {
				count, sum = 2, "0.5"
			}
			labels := fmt.Sprintf(`{kind=%q,outcome=%q}`, kind, outcome)
			require.Contains(
				t,
				body,
				fmt.Sprintf("zinusya_worker_attempt_duration_seconds_count%s %d\n", labels, count),
			)
			require.Contains(t, body, "zinusya_worker_attempt_duration_seconds_sum"+labels+" "+sum+"\n")
		}
	}
	require.Contains(t, body, `zinusya_worker_attempt_duration_seconds_count{kind="other",outcome="other"} 100`)
	require.Contains(t, body, `le="0.25"`)
	require.Equal(t, 37, strings.Count(body, "\nzinusya_worker_attempt_duration_seconds_count{"))
	require.NotContains(t, body, "private-")
	require.Equal(t, globalTrace, otel.GetTracerProvider())
	require.Equal(t, globalMeter, otel.GetMeterProvider())
}
