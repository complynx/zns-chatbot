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
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestModelUsageSampleFailurePrivacyAndUnknowns(t *testing.T) {
	t.Parallel()
	item := credits.UsageObservation{States: [4]int64{0, 1, 1, 0}, Bases: [4]int64{0, 0, 1, 0}}
	for i := range item.Tokens {
		item.Tokens[i].UnknownReceipts = 1
	}
	item.Tokens[0] = credits.UsageTokenObservation{KnownReceipts: 1}
	var fail atomic.Bool
	type deadlineEvidence struct {
		present   bool
		remaining time.Duration
	}
	evidence := make(chan deadlineEvidence, 1)
	var duplicateEvidence atomic.Bool
	assertDeadline := func() {
		t.Helper()
		require.False(t, duplicateEvidence.Load(), "each gather must call the reader once")
		select {
		case captured := <-evidence:
			require.True(t, captured.present)
			require.Positive(t, captured.remaining)
			require.LessOrEqual(t, captured.remaining, 2*time.Second)
		default:
			t.Fatal("collector did not capture its deadline")
		}
	}
	registry := prometheus.NewRegistry()
	require.NoError(
		t,
		registry.Register(newModelUsageCollector(func(ctx context.Context) (credits.UsageObservation, error) {
			deadline, ok := ctx.Deadline()
			select {
			case evidence <- deadlineEvidence{present: ok, remaining: time.Until(deadline)}:
			default:
				duplicateEvidence.Store(true)
			}
			if fail.Load() {
				return credits.UsageObservation{}, errors.New("private-payer receipt-secret")
			}
			return item, nil
		})),
	)
	scrape := func() string {
		response := httptest.NewRecorder()
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
			ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		assertDeadline()
		require.Equal(t, 200, response.Code)
		return response.Body.String()
	}
	text := scrape()
	require.Contains(t, text, `zns_model_usage_sample_attempts{state="dispatched"} 1`)
	require.Contains(t, text, `zns_model_usage_sample_unknown_receipts{kind="cached_input"} 1`)
	require.Contains(t, text, `zns_model_usage_sample_known_receipts{kind="input"} 1`)
	values, err := registry.Gather()
	assertDeadline()
	require.NoError(t, err)
	series := 0
	for _, family := range values {
		series += len(family.GetMetric())
	}
	require.Equal(t, 40, series)
	fail.Store(true)
	text = scrape()
	require.Contains(t, text, "zns_model_usage_sample_available 0")
	require.NotContains(t, text, "zns_model_usage_sample_tokens")
	require.NotContains(t, text, "private")
	require.NotContains(t, text, "receipt-secret")
	fail.Store(false)
	require.Contains(t, scrape(), "zns_model_usage_sample_available 1")
}

func TestModelUsageSampleRejectsInconsistentProjection(t *testing.T) {
	t.Parallel()
	for _, item := range []credits.UsageObservation{
		{Truncated: true}, {States: [4]int64{1025}}, {States: [4]int64{0, 0, 1}},
		{Tokens: [7]credits.UsageTokenObservation{{Sum: 1}}},
		{Tokens: [7]credits.UsageTokenObservation{{Capped: true}}},
	} {
		registry := prometheus.NewRegistry()
		require.NoError(
			t,
			registry.Register(
				newModelUsageCollector(func(context.Context) (credits.UsageObservation, error) { return item, nil }),
			),
		)
		families, err := registry.Gather()
		require.NoError(t, err)
		require.Len(t, families, 1)
		require.Zero(t, families[0].GetMetric()[0].GetGauge().GetValue())
	}
	runtime, err := New(t.Context(), Config{})
	require.NoError(t, err)
	require.Error(t, runtime.RegisterModelUsage(credits.Service{}))
}
