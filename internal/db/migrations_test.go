package db_test

import (
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

func baselineProvider(t *testing.T, s *db.Store) *goose.Provider {
	t.Helper()
	driver := stdlib.OpenDBFromPool(s.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	return provider
}

func migrationHistory(t *testing.T, s *db.Store) string {
	t.Helper()
	var history string
	require.NoError(t, s.Pool().QueryRow(t.Context(),
		"SELECT jsonb_agg(to_jsonb(v) ORDER BY id)::text FROM goose_db_version v").Scan(&history))
	return history
}

func TestBaselineFreshDownUpAndPartialLegacyRefusal(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	provider := baselineProvider(t, s)
	for range 2 {
		var version int64
		require.NoError(t, s.Pool().QueryRow(t.Context(),
			"SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
		require.EqualValues(t, 73, version)
		_, err := provider.DownTo(t.Context(), 0)
		require.NoError(t, err)
		var tables int
		require.NoError(t, s.Pool().QueryRow(t.Context(),
			"SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND starts_with(tablename,'shuttle_')").
			Scan(&tables))
		require.Zero(t, tables)
		_, err = provider.Up(t.Context())
		require.NoError(t, err)
	}
	_, err := provider.DownTo(t.Context(), 0)
	require.NoError(t, err)
	_, err = s.Pool().Exec(t.Context(), "INSERT INTO goose_db_version(version_id,is_applied) VALUES(32,true)")
	require.NoError(t, err)
	before := migrationHistory(t, s)
	require.ErrorContains(t, s.Migrate(t.Context()), "partial legacy schema")
	require.Equal(t, before, migrationHistory(t, s))
	var tables int
	require.NoError(t, s.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND starts_with(tablename,'shuttle_')").
		Scan(&tables))
	require.Zero(t, tables)
}

func TestBaselineKeepsLegacy73HistoryAndAcceptedData(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 1)
	_, err := s.Pool().Exec(t.Context(), `DELETE FROM goose_db_version WHERE version_id<>0;
INSERT INTO goose_db_version(version_id,is_applied) SELECT generate_series(1,73),true;`)
	require.NoError(t, err)
	history := migrationHistory(t, s)
	var objects string
	const objectQuery = "SELECT array_agg(oid ORDER BY oid)::text FROM pg_class WHERE relnamespace=current_schema()::regnamespace"
	require.NoError(t, s.Pool().QueryRow(t.Context(), objectQuery).Scan(&objects))
	require.NoError(t, s.Migrate(t.Context()))
	require.Equal(t, history, migrationHistory(t, s))
	_, err = baselineProvider(t, s).ApplyVersion(t.Context(), 73, false)
	require.ErrorContains(t, err, "legacy migration history must be preserved")
	require.Equal(t, history, migrationHistory(t, s))
	var after string
	require.NoError(t, s.Pool().QueryRow(t.Context(), objectQuery).Scan(&after))
	require.Equal(t, objects, after)
	retained, err := s.GetTask(t.Context(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, retained)
}

func TestBaselineRollbackPreservesAcceptedData(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 1)
	history := migrationHistory(t, s)
	_, err := baselineProvider(t, s).DownTo(t.Context(), 0)
	require.ErrorContains(t, err, "baseline data must be exported before rollback")
	require.Equal(t, history, migrationHistory(t, s))
	retained, err := s.GetTask(t.Context(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, retained)
}
