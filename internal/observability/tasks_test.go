//nolint:testpackage // A controlled clock exercises scrape behavior without real cache-duration sleeps.
package observability

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestTaskMetricsCacheAgeAndSuppressFailedSnapshotsUntilRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	telemetry, err := New(ctx, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	start := time.Unix(1000, 0)
	now := start
	rows := []db.TaskMetric{
		{State: "accepted", Count: 2, AcceptedAgeSeconds: 10},
		{State: "creating_run", Count: 1, AcceptedAgeSeconds: -20},
	}
	calls, failed := 0, false
	require.NoError(t, telemetry.observeTasks(func(context.Context) ([]db.TaskMetric, error) {
		calls++
		if failed {
			now = now.Add(5 * time.Second)
			return nil, errors.New("private database failure")
		}
		return rows, nil
	}, func() time.Time { return now }))
	body := taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_tasks_collection_success 1\n")
	require.Contains(t, body, "\nzinusya_tasks_last_success_timestamp_seconds 1000\n")
	require.Contains(t, body, "zinusya_tasks_unresolved{state=\"accepted\"} 2\n")
	rows[0].Count = 900
	now = start.Add(5 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "zinusya_tasks_unresolved{state=\"accepted\"} 2\n")
	require.Contains(t, body, "zinusya_tasks_oldest_accepted_age_seconds{state=\"accepted\"} 15\n")
	require.Contains(t, body, "zinusya_tasks_oldest_accepted_age_seconds{state=\"creating_run\"} 0\n")
	require.Contains(t, body, "zinusya_tasks_oldest_accepted_age_seconds{state=\"awaiting_user\"} 0\n")
	now = start.Add(25 * time.Second)
	require.Contains(t, taskMetricsBody(t, telemetry),
		"zinusya_tasks_oldest_accepted_age_seconds{state=\"creating_run\"} 5\n")
	require.Equal(t, 1, calls)
	failed = true
	now = start.Add(31 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_tasks_collection_success 0\n")
	require.Contains(t, body, "\nzinusya_tasks_last_success_timestamp_seconds 1000\n")
	require.Contains(t, body, "\nzinusya_tasks_snapshot_age_seconds 36\n")
	require.NotContains(t, body, "\nzinusya_tasks_unresolved{")
	require.NotContains(t, body, "\nzinusya_tasks_oldest_accepted_age_seconds{")
	require.NotContains(t, body, "private database failure")
	failed = false
	rows = []db.TaskMetric{{State: "accepted", Count: 4, AcceptedAgeSeconds: 3}}
	now = start.Add(63 * time.Second)
	require.Contains(t, taskMetricsBody(t, telemetry), "\nzinusya_tasks_collection_success 0\n")
	require.Equal(t, 2, calls)
	now = start.Add(67 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_tasks_collection_success 1\n")
	require.Contains(t, body, "\nzinusya_tasks_last_success_timestamp_seconds 1067\n")
	require.Contains(t, body, "zinusya_tasks_unresolved{state=\"accepted\"} 4\n")
	require.Contains(t, body, "zinusya_tasks_oldest_accepted_age_seconds{state=\"accepted\"} 3\n")
	require.Equal(t, 3, calls)
}

func TestTaskMetricsConcurrentTimeoutKeepsPoolMetricsAndThrottlesFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	telemetry, err := New(ctx, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	pool, err := pgxpool.New(ctx, "postgres://127.0.0.1:1/unused?pool_max_conns=1&sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, telemetry.ObserveDatabasePool(pool))
	var calls atomic.Int32
	require.NoError(t, telemetry.observeTasks(func(query context.Context) ([]db.TaskMetric, error) {
		calls.Add(1)
		<-query.Done()
		return nil, query.Err()
	}, time.Now))
	server := httptest.NewServer(telemetry.MetricsHandler())
	t.Cleanup(server.Close)
	const scrapeCount = 8
	results := make(chan taskScrape, scrapeCount)
	client := &http.Client{Timeout: 2 * time.Second}
	for range scrapeCount {
		go func() { results <- scrapeTaskMetrics(client, server.URL) }()
	}
	for range scrapeCount {
		result := <-results
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.status)
		require.Contains(t, result.body, "\nzinusya_tasks_collection_success 0\n")
		require.Contains(t, result.body, "\nzinusya_tasks_last_success_timestamp_seconds 0\n")
		require.Contains(t, result.body, "\nzinusya_db_pool_max 1\n")
		require.NotContains(t, result.body, "\nzinusya_tasks_unresolved{")
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestTaskMetricsRejectNonfiniteAgesAndUnboundedLabels(t *testing.T) {
	t.Parallel()
	for _, row := range []db.TaskMetric{
		{State: "accepted", Count: 1, AcceptedAgeSeconds: math.Inf(1)},
		{State: "private-user-label", Count: 1, AcceptedAgeSeconds: 0},
	} {
		ctx := context.Background()
		telemetry, err := New(ctx, Options{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
		require.NoError(t, telemetry.observeTasks(func(context.Context) ([]db.TaskMetric, error) {
			return []db.TaskMetric{row}, nil
		}, time.Now))
		body := taskMetricsBody(t, telemetry)
		require.Contains(t, body, "\nzinusya_tasks_collection_success 0\n")
		require.NotContains(t, body, "private-user-label")
		require.NotContains(t, body, "\nzinusya_tasks_unresolved{")
	}
}

func taskMetricsBody(t *testing.T, telemetry *Telemetry) string {
	t.Helper()
	response := httptest.NewRecorder()
	telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	require.Equal(t, http.StatusOK, response.Code)
	return response.Body.String()
}

type taskScrape struct {
	body   string
	status int
	err    error
}

func scrapeTaskMetrics(client *http.Client, endpoint string) taskScrape {
	response, err := client.Get(endpoint)
	if err != nil {
		return taskScrape{err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return taskScrape{body: string(body), status: response.StatusCode, err: err}
}
