package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db"
)

const startupMigrationTimeout = 5 * time.Minute
const startupMigrationConnections = 2

// Migration credentials are used only before composing the runtime. A current
// schema can still start with the limited runtime credential alone.
func migrateAtStartup(parent context.Context, dsn string, runtime *db.Store, output io.Writer) error {
	ctx, cancel := context.WithTimeout(parent, startupMigrationTimeout)
	defer cancel()
	if dsn == "" && runtime.CheckRuntimeAccess(ctx) == nil {
		return nil
	}
	migrator := runtime
	if dsn != "" {
		var err error
		migrator, err = db.Open(ctx, dsn)
		if err != nil {
			return errors.New("open startup migration database")
		}
		defer migrator.Close()
	}
	role, err := verifyMigrationTarget(ctx, runtime, migrator)
	if err != nil {
		return err
	}
	unlock, err := lockMigrationDatabase(ctx, migrator)
	if err != nil {
		return err
	}
	defer unlock()
	if err = migrator.CheckMigrationAccess(ctx, role); err != nil {
		return fmt.Errorf("startup migration preflight: %w", err)
	}
	if err = migrateDatabase(ctx, migrator); err != nil {
		return err
	}
	// Default privileges usually cover new objects. Repair grants only when the
	// runtime contract needs it, using existing settings rather than CLI defaults.
	if err = runtime.CheckRuntimeAccess(ctx); err == nil {
		return nil
	}
	settings, err := migrator.Settings(ctx)
	if err != nil {
		return errors.New("read settings after startup migrations")
	}
	options := db.InstallationOptions{RuntimeRole: role, DefaultLanguage: settings.Document.DefaultLanguage,
		MaxParallelTasks: settings.Document.MaxParallelTasks}
	result, err := migrator.InstallRuntime(ctx, options)
	if err != nil {
		return fmt.Errorf("startup runtime grants: %w", err)
	}
	return json.NewEncoder(output).Encode(result)
}

// Compare actual database/schema and a database-local lock, not endpoint text.
// The random probe detects a different PostgreSQL cluster even if names match.
// Both transactions are read-only except for advisory locks; neither applies DDL.
func verifyMigrationTarget(ctx context.Context, runtime, migrator *db.Store) (string, error) {
	if runtime.Pool() == migrator.Pool() {
		var role string
		err := runtime.Pool().QueryRow(ctx, "SELECT current_user").Scan(&role)
		if err != nil {
			return "", errors.New("inspect runtime database role")
		}
		return role, nil
	}
	source, err := runtime.Pool().Begin(ctx)
	if err != nil {
		return "", errors.New("inspect runtime migration target")
	}
	defer rollbackStartupTransaction(ctx, source)
	target, err := migrator.Pool().Begin(ctx)
	if err != nil {
		return "", errors.New("inspect migration target")
	}
	defer rollbackStartupTransaction(ctx, target)
	var database, schema, role string
	if err = source.QueryRow(ctx, "SELECT current_database(),current_schema(),current_user").
		Scan(&database, &schema, &role); err != nil {
		return "", errors.New("inspect runtime migration target")
	}
	var targetDatabase, targetSchema string
	if err = target.QueryRow(ctx, "SELECT current_database(),current_schema()").
		Scan(&targetDatabase, &targetSchema); err != nil || database != targetDatabase || schema != targetSchema {
		return "", errors.New("migration and runtime database/schema differ")
	}
	probe := "zinusya-migration-target:" + uuid.NewString()
	if _, err = source.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", probe); err != nil {
		return "", errors.New("lock runtime migration target probe")
	}
	var independent bool
	if err = target.QueryRow(ctx, "SELECT pg_try_advisory_xact_lock(hashtextextended($1,0))", probe).
		Scan(&independent); err != nil || independent {
		return "", errors.New("migration and runtime PostgreSQL databases differ")
	}
	return role, nil
}

func rollbackStartupTransaction(parent context.Context, tx pgx.Tx) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(parent), time.Second)
	defer cancel()
	_ = tx.Rollback(cleanup)
}

func openStartupStore(parent context.Context, dsn string, timeout time.Duration) (*db.Store, error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	store, err := db.Open(ctx, dsn)
	if err != nil {
		return nil, errors.New("open application database")
	}
	return store, nil
}

func startupOperationTimeout(migrate bool, probeTimeout time.Duration) time.Duration {
	if migrate {
		return startupMigrationTimeout
	}
	return probeTimeout
}

// Database ACLs are shared across schemas. Both ordinary startup and explicit
// operator migration hold this lock across app/River DDL and runtime grants.
// Goose and River retain their individual transactions and histories.
func lockMigrationDatabase(ctx context.Context, store *db.Store) (func(), error) {
	if store.Pool().Config().MaxConns < startupMigrationConnections {
		return nil, errors.New("migration pool requires at least two connections")
	}
	lock, err := store.Pool().Begin(ctx)
	if err != nil {
		return nil, errors.New("open database migration lock")
	}
	if _, err = lock.Exec(
		ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('zinusya-startup-migrations',0))`,
	); err != nil {
		rollbackStartupTransaction(ctx, lock)
		return nil, errors.New("acquire database migration lock")
	}
	return func() { rollbackStartupTransaction(ctx, lock) }, nil
}
