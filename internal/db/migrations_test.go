package db_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

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

// Baseline DDL takes many PostgreSQL locks. Keep these cases sequential without
// reducing concurrency for unrelated database tests.
func TestBaselineMigrationLifecycle(t *testing.T) {
	t.Parallel()
	baselineFreshDownUpAndPartialLegacyRefusal(t)
	baselineKeepsLegacy73HistoryAndAcceptedData(t)
	baselineRollbackPreservesAcceptedData(t)
	baselineRollbackRefusesEvenUnapplied73History(t)
}

func baselineFreshDownUpAndPartialLegacyRefusal(t *testing.T) {
	t.Helper()
	s := testStore(t)
	provider := baselineProvider(t, s)
	for range 2 {
		var version int64
		require.NoError(t, s.Pool().QueryRow(t.Context(),
			"SELECT max(version_id) FROM goose_db_version WHERE is_applied").Scan(&version))
		require.EqualValues(t, 74, version)
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
	for _, partial := range []int{1, 32, 72} {
		_, err = s.Pool().
			Exec(t.Context(), "INSERT INTO goose_db_version(version_id,is_applied) VALUES($1,true)", partial)
		require.NoError(t, err)
		before := migrationHistory(t, s)
		require.ErrorContains(t, s.Migrate(t.Context()), "partial legacy schema")
		require.Equal(t, before, migrationHistory(t, s))
	}
	var tables int
	require.NoError(t, s.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM pg_tables WHERE schemaname=current_schema() AND starts_with(tablename,'shuttle_')").
		Scan(&tables))
	require.Zero(t, tables)
}

func baselineKeepsLegacy73HistoryAndAcceptedData(t *testing.T) {
	t.Helper()
	s := testStore(t)
	_, err := baselineProvider(t, s).DownTo(t.Context(), 0)
	require.NoError(t, err)
	_, err = s.Pool().Exec(t.Context(), "DELETE FROM goose_db_version WHERE version_id<>0")
	require.NoError(t, err)
	// Use the byte-verified original DDL, without the version-74 extension.
	source, err := migrations.Files.ReadFile("00074_schema.sql")
	require.NoError(t, err)
	_, ddl, ok := strings.Cut(string(source), "-- BEGIN PRESERVED VERSION 73 DDL\n")
	require.True(t, ok)
	ddl, _, ok = strings.Cut(ddl, "-- END PRESERVED VERSION 73 DDL\n")
	require.True(t, ok)
	require.Equal(
		t,
		"b0fb397415e4aca9cf70001a840f7b1cda9e4f2362ea88b57320935f42eca9b2",
		fmt.Sprintf("%x", sha256.Sum256([]byte(ddl))),
	)
	driver := stdlib.OpenDBFromPool(s.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	legacy, err := goose.NewProvider(goose.DialectPostgres, driver, fstest.MapFS{
		"00073_schema.sql": {
			Data: []byte(
				"-- +goose Up\n-- +goose StatementBegin\nDO $legacy73$ BEGIN\n" + ddl + "END $legacy73$;\n-- +goose StatementEnd\n",
			),
		},
	})
	require.NoError(t, err)
	_, err = legacy.Up(t.Context())
	require.NoError(t, err)
	task := accepted(t, s, 1)
	_, err = s.Pool().Exec(t.Context(), `DELETE FROM goose_db_version WHERE version_id>0;
INSERT INTO goose_db_version(version_id,is_applied) SELECT generate_series(1,73),true;
INSERT INTO shuttle_user_profiles(user_id,version,full_name,first_name,last_name,name_source)
SELECT user_id,1,'Анна Иванова','Анна','Иванова','user' FROM shuttle_tasks LIMIT 1;`)
	require.NoError(t, err)
	history := migrationHistory(t, s)
	var originalProfile string
	require.NoError(
		t,
		s.Pool().QueryRow(t.Context(), "SELECT to_jsonb(p)::text FROM shuttle_user_profiles p").Scan(&originalProfile),
	)
	var objects string
	const objectQuery = "SELECT array_agg(oid ORDER BY oid)::text FROM pg_class WHERE " +
		"relnamespace=current_schema()::regnamespace"
	require.NoError(t, s.Pool().QueryRow(t.Context(), objectQuery).Scan(&objects))
	require.NoError(t, s.Migrate(t.Context()))
	var retainedHistory, retainedProfile string
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT jsonb_agg(to_jsonb(v) ORDER BY id)::text FROM goose_db_version v WHERE "+
				"version_id<=73").
			Scan(&retainedHistory),
	)
	require.Equal(t, history, retainedHistory)
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT (to_jsonb(p)-'first_name_latin'-'last_name_latin')::text FROM "+
				"shuttle_user_profiles p").
			Scan(&retainedProfile),
	)
	require.Equal(t, originalProfile, retainedProfile)
	var first, last string
	require.NoError(
		t,
		s.Pool().
			QueryRow(t.Context(), "SELECT first_name_latin,last_name_latin FROM shuttle_user_profiles").
			Scan(&first, &last),
	)
	require.Empty(t, first)
	require.Empty(t, last)
	upgradedHistory := migrationHistory(t, s)
	require.NoError(t, s.Migrate(t.Context()))
	require.Equal(t, upgradedHistory, migrationHistory(t, s))
	_, err = baselineProvider(t, s).ApplyVersion(t.Context(), 74, false)
	require.ErrorContains(t, err, "legacy migration history must be preserved")
	require.Equal(t, upgradedHistory, migrationHistory(t, s))
	var after string
	require.NoError(t, s.Pool().QueryRow(t.Context(), objectQuery).Scan(&after))
	require.Equal(t, objects, after)
	retained, err := s.GetTask(t.Context(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, retained)
}

func TestLatinNameInvalidationSupportsOldAndTransactionalNewWriters(t *testing.T) {
	t.Parallel()
	s := testStore(t)
	task := accepted(t, s, 1)
	_, err := s.Pool().
		Exec(t.Context(), "INSERT INTO shuttle_user_profiles(user_id,version,full_name,first_name,last_name,"+
			"name_source,first_name_latin,last_name_latin)\n"+
			"VALUES($1,1,'Анна Иванова','Анна','Иванова','user','Anna',"+
			"'Ivanova')", task.UserID)
	require.NoError(t, err)
	check := func(first, last string) {
		t.Helper()
		var gotFirst, gotLast string
		require.NoError(
			t,
			s.Pool().
				QueryRow(
					t.Context(),
					"SELECT first_name_latin,last_name_latin FROM shuttle_user_profiles WHERE user_id=$1",
					task.UserID,
				).
				Scan(
					&gotFirst,
					&gotLast,
				),
		)
		require.Equal(t, first, gotFirst)
		require.Equal(t, last, gotLast)
	}
	_, err = s.Pool().
		Exec(t.Context(), `UPDATE shuttle_user_profiles SET first_name=first_name WHERE user_id=$1`, task.UserID)
	require.NoError(t, err)
	check("Anna", "Ivanova")
	_, err = s.Pool().
		Exec(t.Context(), "UPDATE shuttle_user_profiles SET first_name='Мария',full_name='Мария "+
			"Иванова' WHERE user_id=$1", task.UserID)
	require.NoError(t, err)
	check("", "")
	tx, err := s.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(t.Context()) }()
	_, err = tx.Exec(
		t.Context(),
		`UPDATE shuttle_user_profiles SET last_name='Петрова',full_name='Мария Петрова' WHERE user_id=$1`,
		task.UserID,
	)
	require.NoError(t, err)
	_, err = tx.Exec(
		t.Context(),
		`UPDATE shuttle_user_profiles SET first_name_latin='Maria',last_name_latin='Petrova' WHERE user_id=$1`,
		task.UserID,
	)
	require.NoError(t, err)
	require.NoError(t, tx.Commit(t.Context()))
	check("Maria", "Petrova")
}

func baselineRollbackPreservesAcceptedData(t *testing.T) {
	t.Helper()
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

func baselineRollbackRefusesEvenUnapplied73History(t *testing.T) {
	t.Helper()
	s := testStore(t)
	_, err := s.Pool().Exec(t.Context(), `INSERT INTO goose_db_version(version_id,is_applied) VALUES(73,false)`)
	require.NoError(t, err)
	before := migrationHistory(t, s)
	_, err = baselineProvider(t, s).ApplyVersion(t.Context(), 74, false)
	require.ErrorContains(t, err, "legacy migration history must be preserved")
	require.Equal(t, before, migrationHistory(t, s))
	var exists bool
	require.NoError(
		t,
		s.Pool().QueryRow(t.Context(), `SELECT to_regclass('shuttle_user_profiles') IS NOT NULL`).Scan(&exists),
	)
	require.True(t, exists)
}
