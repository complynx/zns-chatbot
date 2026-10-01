package observability

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestAssistantSourceMetricsFreshnessGenerationAndFailure(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	attempt := now.Add(-time.Minute)
	items := []knowledge.SourceStatus{{Slot: knowledge.AssistantAbout, Status: "stale", Version: math.MaxInt64,
		AttemptedAt: &attempt, RefreshedAt: &old, ErrorCode: "fetch_failed"},
		{Slot: knowledge.AssistantQA, Status: "unavailable"}}
	var readErr error
	var boundedDeadline bool
	registry := prometheus.NewRegistry()
	require.NoError(
		t,
		registry.Register(newAssistantSourceCollector(func(ctx context.Context) ([]knowledge.SourceStatus, error) {
			deadline, ok := ctx.Deadline()
			remaining := time.Until(deadline)
			boundedDeadline = ok && remaining > 0 && remaining <= 2*time.Second
			return items, readErr
		}, func() time.Time { return now })),
	)
	scrape := func() string {
		response := httptest.NewRecorder()
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}).
			ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, response.Code)
		return response.Body.String()
	}
	text := scrape()
	require.True(t, boundedDeadline)
	families, err := registry.Gather()
	require.NoError(t, err)
	versionObserved := false
	for _, family := range families {
		if family.GetName() == "zns_assistant_source_version" {
			for _, metric := range family.GetMetric() {
				if metric.GetLabel()[0].GetValue() == knowledge.AssistantAbout {
					versionObserved = true
					require.InDelta(t, float64(1<<53-1), metric.GetGauge().GetValue(), 0)
				}
			}
		}
	}
	require.True(t, versionObserved)
	require.Contains(t, text, `zns_assistant_source_snapshot_available 1`)
	require.Contains(t, text, `zns_assistant_source_refresh_age_seconds{source="assistant_about"} 7200`)
	require.Contains(t, text, `zns_assistant_source_attempt_age_seconds{source="assistant_about"} 60`)
	require.Contains(t, text, `zns_assistant_source_version_capped{source="assistant_about"} 1`)
	require.Contains(t, text, `zns_assistant_source_refresh_age_known{source="assistant_qa"} 0`)
	require.Contains(t, text, `zns_assistant_source_last_error{code="none",source="assistant_qa"} 1`)
	readErr = errors.New("postgres://private-user:private-password@private-query")
	text = scrape()
	require.Contains(t, text, `zns_assistant_source_snapshot_available 0`)
	require.NotContains(t, text, "zns_assistant_source_refresh_age_seconds")
	require.NotContains(t, text, "private")
	readErr = nil
	items = nil
	text = scrape()
	require.Contains(t, text, `zns_assistant_source_snapshot_available 1`)
	require.Contains(t, text, `zns_assistant_source_present{source="assistant_about"} 0`)
	require.NotContains(t, text, "zns_assistant_source_last_error")
}

func TestAssistantSourceMetricsRejectPrivateOrDuplicateVocabulary(t *testing.T) {
	t.Parallel()
	good := knowledge.SourceStatus{Slot: knowledge.AssistantAbout, Status: "ready", Version: 1}
	for _, items := range [][]knowledge.SourceStatus{
		{{Slot: "private-document", Status: "ready"}}, {{Slot: knowledge.AssistantAbout, Status: "private-status"}},
		{{Slot: knowledge.AssistantAbout, Status: "ready", ErrorCode: "private-error"}},
		{{Slot: knowledge.AssistantAbout, Status: "ready", Version: -1}}, {good, good}, {good, good, good},
	} {
		require.False(t, validSourceStatuses(items))
		registry := prometheus.NewRegistry()
		require.NoError(
			t,
			registry.Register(
				newAssistantSourceCollector(
					func(context.Context) ([]knowledge.SourceStatus, error) { return items, nil },
					time.Now,
				),
			),
		)
		families, err := registry.Gather()
		require.NoError(t, err)
		require.Len(t, families, 1)
		require.Zero(t, families[0].GetMetric()[0].GetGauge().GetValue())
	}
	now := time.Now()
	future := now.Add(time.Hour)
	known, age := sourceTimestampAge(now, &future)
	require.InDelta(t, float64(1), known, 0)
	require.Zero(t, age)
	ancient := now.Add(-2 * 365 * 24 * time.Hour)
	_, age = sourceTimestampAge(now, &ancient)
	require.InDelta(t, float64(365*24*60*60), age, 0)
}
