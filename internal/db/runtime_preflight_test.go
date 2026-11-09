package db_test

import (
	"net/url"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
)

func migrateRuntimeQueue(t *testing.T, s *db.Store) {
	t.Helper()
	migrator, err := rivermigrate.New(riverpgxv5.New(s.Pool()), &rivermigrate.Config{Schema: "public"})
	require.NoError(t, err)
	_, err = migrator.Migrate(t.Context(), rivermigrate.DirectionUp, nil)
	require.NoError(t, err)
}

func runtimeRoleStore(t *testing.T, s *db.Store, role string, readOnly bool) *db.Store {
	t.Helper()
	u, err := url.Parse(s.Pool().Config().ConnString())
	require.NoError(t, err)
	query := u.Query()
	query.Set("role", role)
	if readOnly {
		query.Set("default_transaction_read_only", "on")
	}
	u.RawQuery = query.Encode()
	runtime, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	return runtime
}

func TestRuntimePreflightIsReadOnlyAndAllowsAdditionalRights(t *testing.T) {
	t.Parallel()
	s := installationStore(t)
	migrateRuntimeQueue(t, s)
	role := installationRole(t, s)
	_, err := s.InstallRuntime(t.Context(), db.InstallationOptions{
		RuntimeRole: role, DefaultLanguage: "ru", MaxParallelTasks: 8,
	})
	require.NoError(t, err)
	_, err = s.Pool().Exec(t.Context(), "GRANT CREATE ON SCHEMA public TO "+pgx.Identifier{role}.Sanitize())
	require.NoError(t, err)
	runtime := runtimeRoleStore(t, s, role, false)
	var before, after int64
	var calledBefore, calledAfter bool
	err = s.Pool().QueryRow(t.Context(), "SELECT last_value,is_called FROM river_job_id_seq").
		Scan(&before, &calledBefore)
	require.NoError(t, err)
	require.NoError(t, runtime.CheckRuntimeAccess(t.Context()))
	err = s.Pool().QueryRow(t.Context(), "SELECT last_value,is_called FROM river_job_id_seq").
		Scan(&after, &calledAfter)
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.Equal(t, calledBefore, calledAfter)
	var work int
	err = s.Pool().QueryRow(t.Context(), `SELECT (SELECT count(*) FROM shuttle_users)
+(SELECT count(*) FROM shuttle_tasks)+(SELECT count(*) FROM river_job)`).Scan(&work)
	require.NoError(t, err)
	require.Zero(t, work)
}

func TestRuntimePreflightRejectsInsufficientAccess(t *testing.T) {
	t.Parallel()
	s := installationStore(t)
	migrateRuntimeQueue(t, s)
	role := installationRole(t, s)
	_, err := s.InstallRuntime(t.Context(), db.InstallationOptions{
		RuntimeRole: role, DefaultLanguage: "ru", MaxParallelTasks: 8,
	})
	require.NoError(t, err)
	runtime := runtimeRoleStore(t, s, role, false)
	quoted := pgx.Identifier{role}.Sanitize()
	_, err = s.Pool().Exec(t.Context(), "REVOKE DELETE ON shuttle_outbox FROM "+quoted)
	require.NoError(t, err)
	require.ErrorContains(t, runtime.CheckRuntimeAccess(t.Context()), "SELECT, INSERT, UPDATE and DELETE")
	_, err = s.Pool().Exec(t.Context(), "GRANT DELETE ON shuttle_outbox TO "+quoted)
	require.NoError(t, err)
	_, err = s.Pool().Exec(t.Context(), "REVOKE SELECT ON shuttle_knowledge_notice_candidates FROM "+quoted)
	require.NoError(t, err)
	require.ErrorContains(t, runtime.CheckRuntimeAccess(t.Context()), "SELECT on application and River views")
	_, err = s.Pool().Exec(t.Context(), "GRANT SELECT ON shuttle_knowledge_notice_candidates TO "+quoted)
	require.NoError(t, err)
	_, err = s.Pool().Exec(t.Context(), "REVOKE INSERT,UPDATE,DELETE ON shuttle_knowledge_notice_candidates FROM "+quoted)
	require.NoError(t, err)
	require.NoError(t, runtime.CheckRuntimeAccess(t.Context()), "read-only views do not require DML grants")
	_, err = s.Pool().Exec(t.Context(), "REVOKE USAGE,SELECT,UPDATE ON SEQUENCE river_job_id_seq FROM "+quoted)
	require.NoError(t, err)
	require.ErrorContains(t, runtime.CheckRuntimeAccess(t.Context()), "sequence USAGE or UPDATE")
	readOnly := runtimeRoleStore(t, s, role, true)
	require.ErrorContains(t, readOnly.CheckRuntimeAccess(t.Context()), "writable PostgreSQL primary")
}

func TestRuntimePreflightRejectsUnappliedSchema(t *testing.T) {
	t.Parallel()
	s := installationDatabase(t)
	require.ErrorContains(t, s.CheckRuntimeAccess(t.Context()), "application migrations")
	require.NoError(t, s.Migrate(t.Context()))
	require.ErrorContains(t, s.CheckRuntimeAccess(t.Context()), "River queue tables")
	migrateRuntimeQueue(t, s)
	require.NoError(t, s.CheckRuntimeAccess(t.Context()))
	_, err := s.Pool().Exec(t.Context(), "DELETE FROM river_migration WHERE line='main' AND version=8")
	require.NoError(t, err)
	require.ErrorContains(t, s.CheckRuntimeAccess(t.Context()), "current River migrations")
	_, err = s.Pool().Exec(t.Context(), `INSERT INTO goose_db_version(version_id,is_applied)
SELECT version_id,false FROM goose_db_version WHERE is_applied AND version_id>0 ORDER BY version_id DESC LIMIT 1`)
	require.NoError(t, err)
	require.ErrorContains(t, s.CheckRuntimeAccess(t.Context()), "application migrations")
}
