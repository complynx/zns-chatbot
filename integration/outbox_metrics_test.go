package integration_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestOutboxMetricsPoolTimeoutDoesNotInvalidateTaskSnapshot(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	h.stop(t)
	ctx := context.Background()
	parsed, err := url.Parse(h.store.Pool().Config().ConnString())
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("pool_max_conns", "1")
	parsed.RawQuery = query.Encode()
	single, err := db.Open(ctx, parsed.String())
	require.NoError(t, err)
	t.Cleanup(single.Close)
	telemetry, err := observability.New(ctx, observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	require.NoError(t, telemetry.ObserveDatabasePool(single.Pool()))
	require.NoError(t, telemetry.ObserveTasks(h.store))
	require.NoError(t, telemetry.ObserveOutbox(single))
	server := httptest.NewServer(telemetry.MetricsHandler())
	t.Cleanup(server.Close)
	held, err := single.Pool().Acquire(ctx)
	require.NoError(t, err)
	defer held.Release()
	cancelled := single.Pool().Stat().CanceledAcquireCount()
	client := &http.Client{Timeout: 2 * time.Second}
	for attempt := range 2 {
		result := scrapePool(client, server.URL)
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.status)
		require.Contains(t, result.body, "\nzinusya_tasks_collection_success 1\n")
		require.Contains(t, result.body, "\nzinusya_outbox_collection_success 0\n")
		require.NotContains(t, result.body, "\nzinusya_outbox_unresolved{")
		require.Contains(t, result.body, "\nzinusya_db_pool_max 1\n")
		require.Equal(t, cancelled+1, single.Pool().Stat().CanceledAcquireCount())
		if attempt == 0 {
			held.Release()
		}
	}
}

func TestOutboxMetricsBlockedQueryLeavesTaskAndPoolGaugesAvailable(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	h.stop(t)
	ctx := context.Background()
	lock, err := h.store.Pool().Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = lock.Rollback(ctx) })
	_, err = lock.Exec(ctx, "SET LOCAL lock_timeout='2s'")
	require.NoError(t, err)
	_, err = lock.Exec(ctx, "LOCK TABLE shuttle_outbox IN ACCESS EXCLUSIVE MODE")
	require.NoError(t, err)
	telemetry, err := observability.New(ctx, observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	require.NoError(t, telemetry.ObserveDatabasePool(h.store.Pool()))
	require.NoError(t, telemetry.ObserveTasks(h.store))
	require.NoError(t, telemetry.ObserveOutbox(h.store))
	server := httptest.NewServer(telemetry.MetricsHandler())
	t.Cleanup(server.Close)
	result := scrapePool(&http.Client{Timeout: 2 * time.Second}, server.URL)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.status)
	require.Contains(t, result.body, "\nzinusya_tasks_collection_success 1\n")
	require.Contains(t, result.body, "\nzinusya_outbox_collection_success 0\n")
	require.Contains(t, result.body, "\nzinusya_db_pool_max ")
	require.NotContains(t, result.body, "\nzinusya_outbox_unresolved{")
	require.NoError(t, lock.Rollback(ctx))
}
