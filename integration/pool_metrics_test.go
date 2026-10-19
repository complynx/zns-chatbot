package integration_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestDatabasePoolMetricsReflectExhaustionWithoutAcquiringConnections(t *testing.T) {
	t.Parallel()
	h := setup(t, newPeers(t))
	h.stop(t)
	ctx := context.Background()
	config := h.store.Pool().Config()
	config.MaxConns = 1
	config.MinConns = 0
	pool, err := pgxpool.NewWithConfig(ctx, config)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	telemetry, err := observability.New(ctx, observability.Options{})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
	require.NoError(t, telemetry.ObserveDatabasePool(pool))
	server := httptest.NewServer(telemetry.MetricsHandler())
	t.Cleanup(server.Close)
	held, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer held.Release()
	acquires := pool.Stat().AcquireCount()
	const concurrentScrapes = 8
	results := make(chan poolScrape, concurrentScrapes)
	client := &http.Client{Timeout: time.Second}
	for range concurrentScrapes {
		go func() { results <- scrapePool(client, server.URL) }()
	}
	for range concurrentScrapes {
		result := <-results
		require.NoError(t, result.err)
		require.Equal(t, http.StatusOK, result.status)
		for _, value := range []string{"acquired 1", "idle 0", "total 1", "max 1"} {
			require.Contains(t, result.body, "\nzinusya_db_pool_"+value+"\n")
		}
	}
	require.Equal(t, acquires, pool.Stat().AcquireCount())
	held.Release()
	result := scrapePool(client, server.URL)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.status)
	require.Contains(t, result.body, "\nzinusya_db_pool_acquired 0\n")
	require.Contains(t, result.body, "\nzinusya_db_pool_idle 1\n")
	require.Equal(t, acquires, pool.Stat().AcquireCount())
}

func TestTaskMetricAcquisitionDeadlinePreservesPoolMetricsAndCachesFailure(t *testing.T) {
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
	require.NoError(t, telemetry.ObserveTasks(single))
	server := httptest.NewServer(telemetry.MetricsHandler())
	t.Cleanup(server.Close)
	held, err := single.Pool().Acquire(ctx)
	require.NoError(t, err)
	defer held.Release()
	cancelled := single.Pool().Stat().CanceledAcquireCount()
	client := &http.Client{Timeout: 2 * time.Second}
	result := scrapePool(client, server.URL)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.status)
	require.Contains(t, result.body, "\nzinusya_tasks_collection_success 0\n")
	require.Contains(t, result.body, "\nzinusya_db_pool_acquired 1\n")
	require.NotContains(t, result.body, "\nzinusya_tasks_unresolved{")
	require.Equal(t, cancelled+1, single.Pool().Stat().CanceledAcquireCount())
	held.Release()
	result = scrapePool(client, server.URL)
	require.NoError(t, result.err)
	require.Equal(t, http.StatusOK, result.status)
	require.Contains(t, result.body, "\nzinusya_tasks_collection_success 0\n")
	require.Contains(t, result.body, "\nzinusya_db_pool_acquired 0\n")
	require.Equal(t, cancelled+1, single.Pool().Stat().CanceledAcquireCount())
}

type poolScrape struct {
	body   string
	status int
	err    error
}

func scrapePool(client *http.Client, endpoint string) poolScrape {
	response, err := client.Get(endpoint + "/metrics")
	if err != nil {
		return poolScrape{err: err}
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	return poolScrape{body: string(body), status: response.StatusCode, err: err}
}
