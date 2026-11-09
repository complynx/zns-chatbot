package db

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// InstallationOptions applies only to explicit operator migration commands.
// Initial settings never replace an existing settings revision or user data.
type InstallationOptions struct {
	RuntimeRole      string
	DefaultLanguage  string
	MaxParallelTasks int
}

func (o InstallationOptions) Validate() error {
	if o.RuntimeRole == "" || strings.TrimSpace(o.RuntimeRole) != o.RuntimeRole ||
		len(o.RuntimeRole) > 63 || !utf8.ValidString(o.RuntimeRole) || strings.ContainsRune(o.RuntimeRole, 0) ||
		(o.DefaultLanguage != "en" && o.DefaultLanguage != "ru") || o.MaxParallelTasks < 1 || o.MaxParallelTasks > 128 {
		return errors.New("invalid installation options")
	}
	return nil
}

// InstallationResult is safe for deployment logs; it contains no credentials.
type InstallationResult struct {
	RuntimeRole         string `json:"runtime_role"`
	Schema              string `json:"schema"`
	SettingsInitialized bool   `json:"settings_initialized"`
	SettingsVersion     int64  `json:"settings_version"`
}

// InstallRuntime follows app and River migrations using the migrator connection.
// Grants include future objects created by this migrator, so later migrations
// remain usable by the runtime role without another credential-bearing command.
func (s *Store) InstallRuntime(ctx context.Context, o InstallationOptions) (InstallationResult, error) {
	if err := o.Validate(); err != nil {
		return InstallationResult{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return InstallationResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result InstallationResult
	result.RuntimeRole = o.RuntimeRole
	var database string
	if err = tx.QueryRow(ctx, "SELECT current_database(),current_schema()").Scan(&database, &result.Schema); err != nil {
		return result, err
	}
	// Serialize bootstrap and lock the revision before deciding whether to seed.
	if err = tx.QueryRow(ctx, "SELECT version FROM shuttle_settings WHERE id FOR UPDATE").
		Scan(&result.SettingsVersion); err != nil {
		return result, err
	}
	var roleExists bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_roles WHERE rolname=$1)", o.RuntimeRole).Scan(&roleExists)
	if err != nil {
		return result, err
	}
	if !roleExists {
		return result, errors.New("runtime role must already exist")
	}
	role := pgx.Identifier{o.RuntimeRole}.Sanitize()
	schema := pgx.Identifier{result.Schema}.Sanitize()
	databaseName := pgx.Identifier{database}.Sanitize()
	for _, statement := range []string{
		"REVOKE ALL ON DATABASE " + databaseName + " FROM PUBLIC",
		"REVOKE CREATE ON SCHEMA " + schema + " FROM PUBLIC",
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return result, err
		}
	}
	for _, statement := range []string{
		"GRANT CONNECT,TEMPORARY ON DATABASE " + databaseName + " TO " + role,
		"GRANT USAGE ON SCHEMA " + schema + " TO " + role,
		"GRANT SELECT,INSERT,UPDATE,DELETE ON ALL TABLES IN SCHEMA " + schema + " TO " + role,
		"GRANT USAGE,SELECT ON ALL SEQUENCES IN SCHEMA " + schema + " TO " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + schema + " GRANT SELECT,INSERT,UPDATE,DELETE ON TABLES TO " + role,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + schema + " GRANT USAGE,SELECT ON SEQUENCES TO " + role,
	} {
		if _, err = tx.Exec(ctx, statement); err != nil {
			return result, err
		}
	}
	// Wait for existing intake/import writers before the fresh-database check,
	// and keep new writers out until the initial revision is committed.
	if _, err = tx.Exec(ctx, "LOCK TABLE shuttle_users,shuttle_tasks IN SHARE MODE"); err != nil {
		return result, err
	}
	changed, err := tx.Exec(ctx, `WITH before AS (
SELECT version,document FROM shuttle_settings WHERE id
), changed AS (
UPDATE shuttle_settings s SET version=s.version+1,
document=s.document||jsonb_build_object('default_language',$1::text,'max_parallel_tasks',$2::integer)
FROM before b WHERE s.id AND b.version=1 AND s.version=b.version
AND NOT EXISTS(SELECT 1 FROM shuttle_users) AND NOT EXISTS(SELECT 1 FROM shuttle_tasks)
RETURNING s.version,b.document AS old_document,s.document AS new_document
)
INSERT INTO shuttle_settings_audit(actor,version,old_document,new_document)
SELECT 'operator:migrate-installation',version,old_document,new_document FROM changed`,
		o.DefaultLanguage, o.MaxParallelTasks)
	if err != nil {
		return result, err
	}
	result.SettingsInitialized = changed.RowsAffected() == 1
	if err = tx.QueryRow(ctx, "SELECT version FROM shuttle_settings WHERE id").Scan(&result.SettingsVersion); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
