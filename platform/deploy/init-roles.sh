#!/bin/sh
# Sourced by the PostgreSQL entrypoint only when initializing an empty volume.
set -eu
ZNS_MIGRATOR_PASSWORD=$(cat /run/secrets/migrator_password)
ZNS_RUNTIME_PASSWORD=$(cat /run/secrets/runtime_password)
ZNS_ACCOUNTING_PASSWORD=$(cat /run/secrets/accounting_password)
export ZNS_MIGRATOR_PASSWORD ZNS_RUNTIME_PASSWORD ZNS_ACCOUNTING_PASSWORD
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<'SQL'
\getenv migrator_password ZNS_MIGRATOR_PASSWORD
\getenv runtime_password ZNS_RUNTIME_PASSWORD
\getenv accounting_password ZNS_ACCOUNTING_PASSWORD
SELECT current_database() AS database_name \gset
BEGIN;
CREATE ROLE zns_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'migrator_password';
CREATE ROLE zns_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'runtime_password';
CREATE ROLE zns_meter LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'accounting_password';
REVOKE ALL ON DATABASE :"database_name" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"database_name" TO zns_migrator, zns_runtime, zns_meter;
GRANT CREATE ON DATABASE :"database_name" TO zns_migrator;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO zns_migrator;
CREATE SCHEMA core AUTHORIZATION zns_migrator;
CREATE SCHEMA bot AUTHORIZATION zns_migrator;
CREATE SCHEMA interaction AUTHORIZATION zns_migrator;
GRANT USAGE ON SCHEMA core, bot, interaction TO zns_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE zns_migrator IN SCHEMA core, bot, interaction GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO zns_runtime;
ALTER DEFAULT PRIVILEGES FOR ROLE zns_migrator IN SCHEMA core, bot, interaction GRANT USAGE, SELECT ON SEQUENCES TO zns_runtime;
COMMIT;
SQL
unset ZNS_MIGRATOR_PASSWORD ZNS_RUNTIME_PASSWORD ZNS_ACCOUNTING_PASSWORD
