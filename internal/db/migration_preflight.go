package db

import (
	"context"
	"errors"
)

// CheckMigrationAccess runs catalog reads before Goose or River can create anything.
// Authentication is already checked by Open/Ping. External service credentials
// are not used by database migration and are deliberately not sent anywhere.
func (s *Store) CheckMigrationAccess(ctx context.Context, runtimeRole string) error {
	var writable, schemaAccess, databaseOwner, schemaOwner, objectsOwned bool
	err := s.pool.QueryRow(ctx, `SELECT
NOT pg_is_in_recovery() AND current_setting('transaction_read_only')='off',
has_schema_privilege(current_user,current_schema(),'USAGE')
AND has_schema_privilege(current_user,current_schema(),'CREATE'),
pg_has_role(current_user,d.datdba,'USAGE'),
pg_has_role(current_user,n.nspowner,'USAGE'),
NOT EXISTS(SELECT 1 FROM pg_class c WHERE c.relnamespace=n.oid
AND c.relkind IN ('r','p','v','m','f','S') AND NOT pg_has_role(current_user,c.relowner,'USAGE'))
AND NOT EXISTS(SELECT 1 FROM pg_type t WHERE t.typnamespace=n.oid AND t.typtype IN ('e','d')
AND NOT pg_has_role(current_user,t.typowner,'USAGE'))
AND NOT EXISTS(SELECT 1 FROM pg_proc p WHERE p.pronamespace=n.oid
AND p.proname IN ('river_job_notify','river_job_state_in_bitmask')
AND NOT pg_has_role(current_user,p.proowner,'USAGE'))
FROM pg_database d JOIN pg_namespace n ON n.nspname=current_schema()
WHERE d.datname=current_database()`).Scan(&writable, &schemaAccess, &databaseOwner, &schemaOwner, &objectsOwned)
	if err != nil {
		return errors.New("cannot verify migration database privileges")
	}
	if !writable {
		return errors.New("migration requires a writable PostgreSQL primary")
	}
	if !schemaAccess || !objectsOwned {
		return errors.New("migrator requires schema access and ownership of existing migration objects")
	}
	if runtimeRole == "" {
		return nil
	}
	if !databaseOwner || !schemaOwner {
		return errors.New("installation requires ownership of the target database and schema")
	}
	return s.checkInstallationRole(ctx, runtimeRole)
}

func (s *Store) checkInstallationRole(ctx context.Context, role string) error {
	var safe, creates bool
	err := s.pool.QueryRow(ctx, `SELECT
r.rolcanlogin AND NOT (r.rolsuper OR r.rolcreatedb OR r.rolcreaterole OR r.rolreplication OR r.rolbypassrls)
AND r.rolname<>current_user,
pg_has_role(r.oid,n.nspowner,'USAGE') OR EXISTS(
SELECT 1 FROM aclexplode(COALESCE(n.nspacl,acldefault('n',n.nspowner))) a
WHERE a.privilege_type='CREATE' AND CASE WHEN a.grantee=0 THEN false
ELSE pg_has_role(r.oid,a.grantee,'USAGE') END)
FROM pg_roles r JOIN pg_namespace n ON n.nspname=current_schema() WHERE r.rolname=$1`, role).
		Scan(&safe, &creates)
	if err != nil || !safe {
		return errors.New("runtime role must already exist as an unprivileged login")
	}
	if creates {
		return errors.New("runtime role must not own or create objects in the application schema")
	}
	return nil
}
