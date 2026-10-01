-- Fresh allocated cluster only. Password values come from the private helper.
-- Password statements stay out of statement/error logs in this transaction.
SET LOCAL log_statement = 'none';
SET LOCAL log_min_error_statement = 'panic';
\getenv app_password ZNS_REGISTRATION_APP_PASSWORD
\getenv meter_password ZNS_REGISTRATION_METER_PASSWORD
CREATE ROLE zns_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'app_password';
CREATE ROLE zns_meter LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD :'meter_password';
ALTER DATABASE synthetic_qa_zns_registration_fixture OWNER TO zns_app;
REVOKE ALL ON DATABASE synthetic_qa_zns_registration_fixture FROM PUBLIC;
GRANT CONNECT ON DATABASE synthetic_qa_zns_registration_fixture TO zns_app, zns_meter;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
ALTER SCHEMA public OWNER TO zns_app;
CREATE SCHEMA core AUTHORIZATION zns_app;
CREATE SCHEMA bot AUTHORIZATION zns_app;
CREATE SCHEMA interaction AUTHORIZATION zns_app;
\unset app_password
\unset meter_password
