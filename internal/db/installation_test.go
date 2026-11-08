package db_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Installation changes database-wide grants, so these tests use separate databases.
func installationStore(t *testing.T) *db.Store {
	t.Helper()
	dsn := os.Getenv("ZNS_CHATBOT_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("ZNS_CHATBOT_TEST_DATABASE_DSN")
	}
	if dsn == "" {
		t.Skip("ZNS_CHATBOT_TEST_DATABASE_URL is required")
	}
	p, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(p.Close)
	name := "installation_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = p.Exec(t.Context(), "CREATE DATABASE "+pgx.Identifier{name}.Sanitize())
	require.NoError(t, err)
	t.Cleanup(func() {
		_, dropErr := p.Exec(context.Background(), "DROP DATABASE "+pgx.Identifier{name}.Sanitize()+" WITH (FORCE)")
		require.NoError(t, dropErr)
	})
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	u.Path = "/" + name
	s, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(s.Close)
	require.NoError(t, s.Migrate(t.Context()))
	return s
}

func installationRole(t *testing.T, s *db.Store) string {
	t.Helper()
	name := "runtime_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	quoted := pgx.Identifier{name}.Sanitize()
	_, err := s.Pool().Exec(t.Context(), "CREATE ROLE "+quoted+" LOGIN")
	require.NoError(t, err)
	t.Cleanup(func() {
		_, cleanupErr := s.Pool().Exec(context.Background(), "DROP OWNED BY "+quoted)
		require.NoError(t, cleanupErr)
		_, cleanupErr = s.Pool().Exec(context.Background(), "DROP ROLE "+quoted)
		require.NoError(t, cleanupErr)
	})
	return name
}

func TestInstallationGrantsAndSeedsOnce(t *testing.T) {
	t.Parallel()
	s := installationStore(t)
	role := installationRole(t, s)
	o := db.InstallationOptions{RuntimeRole: role, DefaultLanguage: "ru", MaxParallelTasks: 8}
	result, err := s.InstallRuntime(t.Context(), o)
	require.NoError(t, err)
	require.True(t, result.SettingsInitialized)
	require.EqualValues(t, 2, result.SettingsVersion)
	settings, err := s.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, "ru", settings.Document.DefaultLanguage)
	require.Equal(t, 8, settings.Document.MaxParallelTasks)
	var create, connect, table bool
	err = s.Pool().QueryRow(t.Context(), `SELECT has_schema_privilege($1,'public','CREATE'),
has_database_privilege($1,current_database(),'CONNECT'),has_table_privilege($1,'shuttle_settings','UPDATE')`, role).
		Scan(&create, &connect, &table)
	require.NoError(t, err)
	require.False(t, create)
	require.True(t, connect)
	require.True(t, table)
	_, err = s.Pool().Exec(t.Context(), "CREATE TABLE future_runtime(id bigserial PRIMARY KEY,value text)")
	require.NoError(t, err)
	tx, err := s.Pool().Begin(t.Context())
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(context.Background()) }()
	_, err = tx.Exec(t.Context(), "SET LOCAL ROLE "+pgx.Identifier{role}.Sanitize())
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), "INSERT INTO future_runtime(value) VALUES('works')")
	require.NoError(t, err)
	_, err = tx.Exec(t.Context(), "CREATE TABLE forbidden_runtime(id int)")
	require.Error(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
	settings.Document.DefaultLanguage = "en"
	settings.Document.MaxParallelTasks = 3
	updated, err := s.CompareAndSwapSettings(t.Context(), settings.Version, settings.Document, "operator:test")
	require.NoError(t, err)
	retry, err := s.InstallRuntime(t.Context(), o)
	require.NoError(t, err)
	require.False(t, retry.SettingsInitialized)
	require.Equal(t, updated.Version, retry.SettingsVersion)
	retained, err := s.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, updated, retained)
	var audits int
	err = s.Pool().QueryRow(t.Context(),
		"SELECT count(*) FROM shuttle_settings_audit WHERE actor='operator:migrate-installation'").Scan(&audits)
	require.NoError(t, err)
	require.Equal(t, 1, audits)
}

func TestInstallationPreservesUsedDefaultsAndRejectsUnsafeRoles(t *testing.T) {
	t.Parallel()
	s := installationStore(t)
	role := installationRole(t, s)
	accepted(t, s, 1)
	before, err := s.Settings(t.Context())
	require.NoError(t, err)
	o := db.InstallationOptions{RuntimeRole: role, DefaultLanguage: "ru", MaxParallelTasks: 8}
	result, err := s.InstallRuntime(t.Context(), o)
	require.NoError(t, err)
	require.False(t, result.SettingsInitialized)
	after, err := s.Settings(t.Context())
	require.NoError(t, err)
	require.Equal(t, before, after)
	_, err = s.Pool().Exec(t.Context(), "ALTER ROLE "+pgx.Identifier{role}.Sanitize()+" CREATEDB")
	require.NoError(t, err)
	_, err = s.InstallRuntime(t.Context(), o)
	require.ErrorContains(t, err, "unprivileged login")
	o.RuntimeRole = "missing_installation_role"
	_, err = s.InstallRuntime(t.Context(), o)
	require.ErrorContains(t, err, "must already exist")
	o.RuntimeRole = role
	o.DefaultLanguage = "invalid"
	_, err = s.InstallRuntime(t.Context(), o)
	require.ErrorContains(t, err, "invalid installation options")
}
