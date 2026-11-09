package db

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"github.com/complynx/zns-chatbot/migrations"
)

// CheckRuntimeAccess verifies the installed schema and the configured runtime
// grant contract. It never migrates, writes a row or advances a sequence.
func (s *Store) CheckRuntimeAccess(ctx context.Context) error {
	var writable bool
	if err := s.pool.QueryRow(ctx, `SELECT NOT pg_is_in_recovery()
AND current_setting('transaction_read_only')='off'`).Scan(&writable); err != nil {
		return errors.New("cannot verify runtime database access")
	}
	if !writable {
		return errors.New("runtime requires a writable PostgreSQL primary")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return errors.New("cannot inspect runtime database")
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var schema string
	var usage bool
	err = tx.QueryRow(ctx, `SELECT COALESCE(current_schema(),''),
COALESCE(has_schema_privilege(current_user,current_schema(),'USAGE'),false)`).Scan(&schema, &usage)
	if err != nil || !usage {
		return errors.New("runtime requires application schema USAGE")
	}
	if err = checkRuntimeApplication(ctx, tx, schema); err != nil {
		return err
	}
	if err = checkRuntimeGrants(ctx, tx, schema); err != nil {
		return err
	}
	migrator, err := rivermigrate.New(riverpgxv5.New(s.pool), &rivermigrate.Config{Schema: schema})
	if err != nil {
		return errors.New("cannot inspect runtime queue schema")
	}
	queue, err := migrator.ValidateTx(ctx, tx, nil)
	if err != nil || !queue.OK {
		return errors.New("runtime requires current River migrations; run -migrate first")
	}
	return nil
}

func checkRuntimeApplication(ctx context.Context, tx pgx.Tx, schema string) error {
	latest, err := latestRuntimeMigration()
	if err != nil {
		return err
	}
	var version int64
	err = tx.QueryRow(ctx, `SELECT COALESCE(max(version_id),0) FROM (
SELECT DISTINCT ON (version_id) version_id,is_applied FROM `+
		pgx.Identifier{schema, "goose_db_version"}.Sanitize()+`
ORDER BY version_id,id DESC) history WHERE is_applied AND version_id>0`).Scan(&version)
	if err != nil || version < latest {
		return errors.New("runtime requires current application migrations and readable history; run -migrate first")
	}
	return nil
}

func latestRuntimeMigration() (int64, error) {
	files, err := fs.ReadDir(migrations.Files, ".")
	if err != nil {
		return 0, errors.New("cannot inspect embedded application migrations")
	}
	var latest int64
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}
		prefix, _, _ := strings.Cut(file.Name(), "_")
		version, parseErr := strconv.ParseInt(prefix, 10, 64)
		if parseErr != nil {
			return 0, errors.New("invalid embedded application migration version")
		}
		latest = max(latest, version)
	}
	if latest == 0 {
		return 0, errors.New("no embedded application migrations")
	}
	return latest, nil
}

func checkRuntimeGrants(ctx context.Context, tx pgx.Tx, schema string) error {
	var tables, sequences, views, queue bool
	err := tx.QueryRow(ctx, `SELECT
NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname=$1 AND c.relkind IN ('r','p')
AND (starts_with(c.relname,'shuttle_') OR starts_with(c.relname,'river_'))
AND NOT (has_table_privilege(current_user,c.oid,'SELECT')
AND has_table_privilege(current_user,c.oid,'INSERT')
AND has_table_privilege(current_user,c.oid,'UPDATE')
AND has_table_privilege(current_user,c.oid,'DELETE'))),
NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname=$1 AND c.relkind='S'
AND (starts_with(c.relname,'shuttle_') OR starts_with(c.relname,'river_'))
AND NOT (has_sequence_privilege(current_user,c.oid,'USAGE')
OR has_sequence_privilege(current_user,c.oid,'UPDATE'))),
NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname=$1 AND c.relkind IN ('v','m')
AND (starts_with(c.relname,'shuttle_') OR starts_with(c.relname,'river_'))
AND NOT has_table_privilege(current_user,c.oid,'SELECT')),
NOT EXISTS(SELECT 1 FROM unnest(ARRAY['shuttle_settings','shuttle_users','shuttle_tasks',
'shuttle_deployment','shuttle_inbox','shuttle_outbox','river_migration','river_job',
'river_queue','river_leader','river_notification']) required(name)
WHERE NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname=$1 AND c.relname=required.name AND c.relkind IN ('r','p')))`, schema).
		Scan(&tables, &sequences, &views, &queue)
	if err != nil {
		return errors.New("cannot verify runtime table and sequence access")
	}
	if !queue {
		return errors.New("runtime requires application and River queue tables; run -migrate first")
	}
	if !tables {
		return errors.New("runtime requires SELECT, INSERT, UPDATE and DELETE on application and River tables")
	}
	if !sequences {
		return errors.New("runtime requires sequence USAGE or UPDATE for application and River inserts")
	}
	if !views {
		return errors.New("runtime requires SELECT on application and River views")
	}
	return nil
}
