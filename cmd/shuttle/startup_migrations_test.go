package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/migrations"
)

type startupMigrationFixture struct {
	owner    *db.Store
	runtime  *db.Store
	ownerURL string
	role     string
	schema   string
}

func startupMigrationStore(t *testing.T) startupMigrationFixture {
	t.Helper()
	dsn := os.Getenv("ZNS_CHATBOT_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("real PostgreSQL fixture is required")
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(admin.Close)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	database := "startup_db_" + suffix
	_, err = admin.Exec(t.Context(), "CREATE DATABASE "+database)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, dropErr := admin.Exec(ctx, "DROP DATABASE "+database+" WITH (FORCE)")
		require.NoError(t, dropErr)
	})
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	parsed.Path = "/" + database
	dsn = parsed.String()
	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	schema, role := "startup_"+suffix, "runtime_"+suffix
	_, err = pool.Exec(t.Context(), "CREATE SCHEMA "+schema+"; CREATE ROLE "+role+" NOLOGIN; "+
		"GRANT USAGE ON SCHEMA "+schema+" TO "+role)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, cleanupErr := pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE; DROP OWNED BY "+role+"; DROP ROLE "+role)
		require.NoError(t, cleanupErr)
	})
	u, err := url.Parse(dsn)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	ownerURL := u.String()
	owner, err := db.Open(t.Context(), ownerURL)
	require.NoError(t, err)
	t.Cleanup(owner.Close)
	q.Set("role", role)
	u.RawQuery = q.Encode()
	runtime, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	return startupMigrationFixture{owner, runtime, ownerURL, role, schema}
}

func installStartupFixture(t *testing.T, f startupMigrationFixture) {
	t.Helper()
	require.NoError(t, migrateDatabase(t.Context(), f.owner))
	_, err := f.owner.InstallRuntime(t.Context(), db.InstallationOptions{
		RuntimeRole: f.role, DefaultLanguage: "ru", MaxParallelTasks: 3})
	require.NoError(t, err)
}

func startupMigrationSnapshot(t *testing.T, store *db.Store) string {
	t.Helper()
	var snapshot string
	err := store.Pool().QueryRow(t.Context(), `SELECT jsonb_build_object(
 'users',(SELECT jsonb_agg(to_jsonb(u) ORDER BY id) FROM shuttle_users u),
 'tasks',(SELECT jsonb_agg(to_jsonb(t) ORDER BY id) FROM shuttle_tasks t),
 'outbox',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM shuttle_outbox o),
 'settings',(SELECT jsonb_agg(to_jsonb(s)) FROM shuttle_settings s))::text`).Scan(&snapshot)
	require.NoError(t, err)
	return snapshot
}

func TestStartupMigrationsUpgradePreservesDataAndRepairsRuntimeGrants(t *testing.T) {
	t.Parallel()
	f := startupMigrationStore(t)
	installStartupFixture(t, f)
	task, _, err := f.owner.Intake(t.Context(), db.IntakeRequest{BotID: 1, UpdateID: 1,
		TelegramUserID: 42, ChatID: 43, Payload: json.RawMessage(`{"text":"retained"}`)}, nil)
	require.NoError(t, err)
	_, err = f.owner.Pool().Exec(t.Context(), `INSERT INTO shuttle_outbox(id,task_id,dedup_key,chunk_seq,payload,state)
 VALUES($1,$2,'retained-unknown',0,'{}','delivery_unknown')`, uuid.New(), task.ID)
	require.NoError(t, err)
	_, err = f.owner.Pool().Exec(t.Context(), `UPDATE shuttle_settings
 SET version=5,document=document||'{"default_language":"ru","max_parallel_tasks":3}'`)
	require.NoError(t, err)
	// Real upgrade from the installed previous application schema, not a fresh schema.
	driver := stdlib.OpenDBFromPool(f.owner.Pool())
	t.Cleanup(func() { require.NoError(t, driver.Close()) })
	provider, err := goose.NewProvider(goose.DialectPostgres, driver, migrations.Files)
	require.NoError(t, err)
	_, err = provider.DownTo(t.Context(), 78)
	require.NoError(t, err)
	_, err = f.owner.Pool().Exec(t.Context(), "ALTER DEFAULT PRIVILEGES IN SCHEMA "+f.schema+
		" REVOKE ALL ON TABLES FROM "+f.role)
	require.NoError(t, err)
	before := startupMigrationSnapshot(t, f.owner)
	require.NoError(t, migrateAtStartup(t.Context(), f.ownerURL, f.runtime, &bytes.Buffer{}))
	require.NoError(t, f.runtime.CheckRuntimeAccess(t.Context()))
	require.Equal(t, before, startupMigrationSnapshot(t, f.owner))
	// A limited runtime restarts without migration credentials once schema/grants are current.
	require.Error(t, f.runtime.CheckMigrationAccess(t.Context(), f.role))
	require.NoError(t, migrateAtStartup(t.Context(), "", f.runtime, &bytes.Buffer{}))
	require.Equal(t, before, startupMigrationSnapshot(t, f.owner))
}

func TestStartupMigrationsRejectWrongTargetAndInsufficientFallbackBeforeDDL(t *testing.T) {
	t.Parallel()
	f := startupMigrationStore(t)
	wrong, err := url.Parse(f.ownerURL)
	require.NoError(t, err)
	q := wrong.Query()
	q.Set("search_path", "public")
	wrong.RawQuery = q.Encode()
	require.ErrorContains(
		t,
		migrateAtStartup(t.Context(), wrong.String(), f.runtime, &bytes.Buffer{}),
		"database/schema differ",
	)
	require.ErrorContains(t, migrateAtStartup(t.Context(), "", f.runtime, &bytes.Buffer{}), "migration preflight")
	var present bool
	require.NoError(t, f.owner.Pool().QueryRow(t.Context(),
		"SELECT to_regclass('goose_db_version') IS NOT NULL").Scan(&present))
	require.False(t, present, "invalid credentials/target cannot create migration history")
}

func TestStartupMigrationsSerializeConcurrentBoots(t *testing.T) {
	t.Parallel()
	f := startupMigrationStore(t)
	var workers sync.WaitGroup
	outcomes := make(chan error, 2)
	for range 2 {
		workers.Go(func() { outcomes <- migrateAtStartup(t.Context(), f.ownerURL, f.runtime, &bytes.Buffer{}) })
	}
	workers.Wait()
	close(outcomes)
	for err := range outcomes {
		require.NoError(t, err)
	}
	require.NoError(t, f.runtime.CheckRuntimeAccess(t.Context()))
}

func TestStartupMigrationsSerializeDatabaseACLsAcrossSchemas(t *testing.T) {
	t.Parallel()
	first := startupMigrationStore(t)
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	schema, role := "startup_"+suffix, "runtime_"+suffix
	_, err := first.owner.Pool().Exec(t.Context(), "CREATE SCHEMA "+schema+"; CREATE ROLE "+role+" NOLOGIN; "+
		"GRANT USAGE ON SCHEMA "+schema+" TO "+role)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, cleanupErr := first.owner.Pool().
			Exec(ctx, "DROP SCHEMA "+schema+" CASCADE; DROP OWNED BY "+role+"; DROP ROLE "+role)
		require.NoError(t, cleanupErr)
	})
	u, err := url.Parse(first.ownerURL)
	require.NoError(t, err)
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	ownerURL := u.String()
	q.Set("role", role)
	u.RawQuery = q.Encode()
	runtime, err := db.Open(t.Context(), u.String())
	require.NoError(t, err)
	t.Cleanup(runtime.Close)
	var workers sync.WaitGroup
	outcomes := make(chan error, 2)
	workers.Go(func() { outcomes <- migrateAtStartup(t.Context(), first.ownerURL, first.runtime, &bytes.Buffer{}) })
	workers.Go(func() { outcomes <- migrateAtStartup(t.Context(), ownerURL, runtime, &bytes.Buffer{}) })
	workers.Wait()
	close(outcomes)
	for outcome := range outcomes {
		require.NoError(t, outcome)
	}
	require.NoError(t, first.runtime.CheckRuntimeAccess(t.Context()))
	require.NoError(t, runtime.CheckRuntimeAccess(t.Context()))
}

func TestStartupMigrationsSerializeOperatorAndAutomaticBoot(t *testing.T) {
	t.Parallel()
	f := startupMigrationStore(t)
	var workers sync.WaitGroup
	outcomes := make(chan error, 2)
	workers.Go(func() { outcomes <- migrateAtStartup(t.Context(), f.ownerURL, f.runtime, &bytes.Buffer{}) })
	workers.Go(func() {
		outcomes <- migrateAndInstall(t.Context(), f.owner, db.InstallationOptions{
			RuntimeRole: f.role, DefaultLanguage: "en", MaxParallelTasks: 8}, &bytes.Buffer{})
	})
	workers.Wait()
	close(outcomes)
	for outcome := range outcomes {
		require.NoError(t, outcome)
	}
	require.NoError(t, f.runtime.CheckRuntimeAccess(t.Context()))
}
