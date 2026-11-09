package db_test

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func TestMigrationPreflightRejectsRolesBeforeDDL(t *testing.T) {
	t.Parallel()
	s := installationDatabase(t)
	role := installationRole(t, s)
	ctx := t.Context()
	require.ErrorContains(t, s.CheckMigrationAccess(ctx, "missing_"+role), "must already exist")
	quoted := pgx.Identifier{role}.Sanitize()
	_, err := s.Pool().Exec(ctx, "ALTER ROLE "+quoted+" CREATEDB")
	require.NoError(t, err)
	_, err = s.Pool().Exec(ctx, "GRANT CREATE ON SCHEMA public TO "+quoted)
	require.NoError(t, err)
	require.NoError(t, s.CheckMigrationAccess(ctx, role))
	var count int
	err = s.Pool().QueryRow(ctx, "SELECT count(*) FROM pg_class WHERE relnamespace='public'::regnamespace").Scan(&count)
	require.NoError(t, err)
	require.Zero(t, count, "preflight must not create Goose, River or application objects")
}

func TestMigrationPreflightRejectsReadOnly(t *testing.T) {
	t.Parallel()
	s := installationDatabase(t)
	u, err := url.Parse(s.Pool().Config().ConnString())
	require.NoError(t, err)
	query := u.Query()
	query.Set("default_transaction_read_only", "on")
	u.RawQuery = query.Encode()
	readOnly, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(readOnly.Close)
	require.ErrorContains(t, readOnly.CheckMigrationAccess(t.Context(), ""), "writable PostgreSQL primary")
}
