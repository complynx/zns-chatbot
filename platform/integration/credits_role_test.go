package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/credits"
)

func TestCreditsRestrictedRuntimeGrants(t *testing.T) {
	t.Parallel()
	db := database(t)
	installCreditPrice(t, credits.Service{DB: db})
	cfg := db.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, err := conn.Exec(ctx, "SET ROLE zns_bot")
		return err
	}
	restricted, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(restricted.Close)
	service := credits.Service{DB: restricted, Enforce: true}
	attempt := creditAttempt("alice")
	require.NoError(t, service.Reserve(t.Context(), attempt))
	require.NoError(t, service.Dispatch(t.Context(), attempt.ID))
	require.NoError(
		t,
		service.Settle(
			t.Context(),
			attempt.ID,
			credits.Settlement{Usage: credits.Usage{Basis: "unknown"}, CostBasis: "unknown"},
		),
	)
	usage, err := service.Usage(t.Context(), "alice", "alice")
	require.NoError(t, err)
	require.Equal(t, int64(600_000_000), usage.HeldNanoUSD)
	for _, statement := range []string{`UPDATE credits.accounts SET unlimited=true`, `SELECT * FROM core.orders LIMIT 1`, `UPDATE credits.price_selection SET version=version`, `UPDATE credits.default_policy SET version=version`, `DELETE FROM credits.attempts`} {
		_, err = restricted.Exec(t.Context(), statement)
		var denied *pgconn.PgError
		require.ErrorAs(t, err, &denied)
		require.Equal(t, "42501", denied.Code)
	}
}
