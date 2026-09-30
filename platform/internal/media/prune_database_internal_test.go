package media

import (
	"context"
	"io"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func TestPruneExpiredPreservesSafeDatabaseFailure(t *testing.T) {
	t.Parallel()
	config, err := pgxpool.ParseConfig("postgres://private-user@127.0.0.1/private-db?sslmode=disable")
	require.NoError(t, err)
	config.BeforeConnect = func(context.Context, *pgx.ConnConfig) error { return io.ErrUnexpectedEOF }
	db, err := pgxpool.NewWithConfig(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(db.Close)
	count, err := (Service{DB: db}).PruneExpired(t.Context())
	require.Zero(t, count)
	require.Equal(t, core.ErrDatabase, err)
}
