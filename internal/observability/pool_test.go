package observability_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/observability"
)

func TestDatabasePoolMetricsAreScopedAndNeedNoDatabaseConnection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, maximum := range []string{"2", "3"} {
		pool, err := pgxpool.New(ctx,
			"postgres://metrics_user:private_metrics_password@127.0.0.1:1/private_metrics_database?"+
				"sslmode=disable&pool_max_conns="+maximum,
		)
		require.NoError(t, err)
		t.Cleanup(pool.Close)
		telemetry, err := observability.New(ctx, observability.Options{})
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, telemetry.Shutdown(ctx)) })
		require.NoError(t, telemetry.ObserveDatabasePool(pool))
		response := httptest.NewRecorder()
		telemetry.MetricsHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
		require.Equal(t, http.StatusOK, response.Code)
		for _, value := range []string{"acquired 0", "idle 0", "total 0", "max " + maximum} {
			require.Contains(t, response.Body.String(), "\nzinusya_db_pool_"+value+"\n")
		}
		for _, private := range []string{"metrics_user", "private_metrics_password", "private_metrics_database"} {
			require.NotContains(t, response.Body.String(), private)
		}
		require.Zero(t, pool.Stat().AcquireCount())
	}
}
