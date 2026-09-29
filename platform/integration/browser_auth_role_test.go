package integration_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func TestBrowserUsernameLookupKeepsBotOutsideCoreSQL(t *testing.T) {
	t.Parallel()
	f, s := browserFixture(t)
	_, err := f.db.Exec(
		t.Context(),
		`GRANT USAGE ON SCHEMA bot TO zns_bot; GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA bot TO zns_bot`,
	)
	require.NoError(t, err)
	cfg := f.db.Config()
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, roleErr := conn.Exec(ctx, "SET ROLE zns_bot")
		return roleErr
	}
	restricted, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(restricted.Close)
	_, err = restricted.Exec(t.Context(), `SELECT username FROM core.users LIMIT 1`)
	require.Error(t, err)
	s.DB = restricted
	_, _ = startBrowser(t, s)
}
