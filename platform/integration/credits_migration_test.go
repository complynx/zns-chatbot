package integration_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/store"
)

func TestCreditsMigrationAsRestrictedOwner(t *testing.T) {
	t.Parallel()
	address := os.Getenv("TEST_DATABASE_URL")
	if address == "" {
		t.Skip("disposable PostgreSQL required")
	}
	admin, err := pgxpool.New(t.Context(), address)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	name := "zns_credit_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	_, err = admin.Exec(t.Context(), "CREATE ROLE "+quoted+" NOLOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP ROLE "+quoted)
		assert.NoError(t, cleanErr)
	})
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+quoted+" OWNER "+quoted)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanErr := admin.Exec(context.WithoutCancel(t.Context()), "DROP DATABASE "+quoted+" WITH (FORCE)")
		assert.NoError(t, cleanErr)
	})
	cfg, err := pgxpool.ParseConfig(address)
	require.NoError(t, err)
	cfg.ConnConfig.Database = name
	cfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		_, roleErr := conn.Exec(ctx, "SET ROLE "+quoted)
		return roleErr
	}
	owner, err := pgxpool.NewWithConfig(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(owner.Close)
	var elevated bool
	require.NoError(
		t,
		owner.QueryRow(t.Context(), `SELECT rolsuper OR rolcreaterole OR rolcreatedb FROM pg_roles WHERE rolname=current_user`).
			Scan(&elevated),
	)
	require.False(t, elevated)
	require.NoError(t, store.Migrate(t.Context(), owner))
	var allowed bool
	require.NoError(
		t,
		owner.QueryRow(t.Context(), `SELECT has_table_privilege('zns_bot','credits.attempts','INSERT')`).Scan(&allowed),
	)
	require.True(t, allowed)
}
