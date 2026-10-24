//nolint:testpackage // A controlled clock exercises scrape behavior without real cache-duration sleeps.
package observability

import (
	"context"
	"errors"
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

func TestOutboxMetricsCacheAgeAndSuppressFailedSnapshotsUntilRecovery(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	telemetry, err := New(ctx, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	start := time.Unix(1000, 0)
	now := start
	rows := []db.OutboxMetric{
		{State: "pending", Count: 2, CreatedAgeSeconds: 10},
		{State: "retryable", Count: 1, CreatedAgeSeconds: -20},
	}
	calls, failed := 0, false
	require.NoError(t, telemetry.observeOutbox(func(context.Context) ([]db.OutboxMetric, error) {
		calls++
		if failed {
			now = now.Add(5 * time.Second)
			return nil, errors.New("private database failure")
		}
		return rows, nil
	}, func() time.Time { return now }))
	body := taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_outbox_collection_success 1\n")
	require.Contains(t, body, "\nzinusya_outbox_last_success_timestamp_seconds 1000\n")
	require.Contains(t, body, `zinusya_outbox_unresolved{state="pending"} 2`+"\n")
	rows[0].Count = 900
	now = start.Add(5 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, `zinusya_outbox_unresolved{state="pending"} 2`+"\n")
	require.Contains(t, body, `zinusya_outbox_oldest_created_age_seconds{state="pending"} 15`+"\n")
	require.Contains(t, body, `zinusya_outbox_oldest_created_age_seconds{state="retryable"} 0`+"\n")
	require.Contains(t, body, `zinusya_outbox_oldest_created_age_seconds{state="send_inflight"} 0`+"\n")
	now = start.Add(25 * time.Second)
	require.Contains(t, taskMetricsBody(t, telemetry),
		`zinusya_outbox_oldest_created_age_seconds{state="retryable"} 5`+"\n")
	require.Equal(t, 1, calls)
	failed = true
	now = start.Add(31 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_outbox_collection_success 0\n")
	require.Contains(t, body, "\nzinusya_outbox_last_success_timestamp_seconds 1000\n")
	require.Contains(t, body, "\nzinusya_outbox_snapshot_age_seconds 36\n")
	require.NotContains(t, body, "\nzinusya_outbox_unresolved{")
	require.NotContains(t, body, "\nzinusya_outbox_oldest_created_age_seconds{")
	require.NotContains(t, body, "private database failure")
	failed = false
	rows = []db.OutboxMetric{{State: "pending", Count: 4, CreatedAgeSeconds: 3}}
	now = start.Add(63 * time.Second)
	require.Contains(t, taskMetricsBody(t, telemetry), "\nzinusya_outbox_collection_success 0\n")
	require.Equal(t, 2, calls)
	now = start.Add(67 * time.Second)
	body = taskMetricsBody(t, telemetry)
	require.Contains(t, body, "\nzinusya_outbox_collection_success 1\n")
	require.Contains(t, body, "\nzinusya_outbox_last_success_timestamp_seconds 1067\n")
	require.Contains(t, body, `zinusya_outbox_unresolved{state="pending"} 4`+"\n")
	require.Contains(t, body, `zinusya_outbox_oldest_created_age_seconds{state="pending"} 3`+"\n")
	require.Equal(t, 3, calls)
}

func TestOutboxMetricsConcurrentTimeoutKeepsPoolMetricsAndThrottlesFailure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	telemetry, err := New(ctx, Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	pool, err := pgxpool.New(ctx, "postgres://127.0.0.1:1/unused?pool_max_conns=1&sslmode=disable")
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	require.NoError(t, telemetry.ObserveDatabasePool(pool))
	require.NoError(t, telemetry.observeTasks(func(context.Context) ([]db.TaskMetric, error) {
		return nil, nil
	}, time.Now))
	var calls atomic.Int32
	require.NoError(t, telemetry.observeOutbox(func(query context.Context) ([]db.OutboxMetric, error) {
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
		require.Contains(t, result.body, "\nzinusya_outbox_collection_success 0\n")
		require.Contains(t, result.body, "\nzinusya_outbox_last_success_timestamp_seconds 0\n")
		require.Contains(t, result.body, "\nzinusya_db_pool_max 1\n")
		require.Contains(t, result.body, "\nzinusya_tasks_collection_success 1\n")
		require.NotContains(t, result.body, "\nzinusya_outbox_unresolved{")
	}
	require.EqualValues(t, 1, calls.Load())
}

func TestOutboxMetricsRejectNonfiniteAgesAndUnboundedLabels(t *testing.T) {
	t.Parallel()
	for _, row := range []db.OutboxMetric{
		{State: "pending", Count: 1, CreatedAgeSeconds: math.Inf(1)},
		{State: "private-user-label", Count: 1, CreatedAgeSeconds: 0},
	} {
		ctx := context.Background()
		telemetry, err := New(ctx, Options{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
		require.NoError(t, telemetry.observeOutbox(func(context.Context) ([]db.OutboxMetric, error) {
			return []db.OutboxMetric{row}, nil
		}, time.Now))
		body := taskMetricsBody(t, telemetry)
		require.Contains(t, body, "\nzinusya_outbox_collection_success 0\n")
		require.NotContains(t, body, "private-user-label")
		require.NotContains(t, body, "\nzinusya_outbox_unresolved{")
	}
}
