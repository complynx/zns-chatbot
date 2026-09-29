CREATE TABLE credits.default_policy (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 monthly_nano_usd bigint NOT NULL CHECK(monthly_nano_usd>=0),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0)
);
INSERT INTO credits.default_policy(singleton,monthly_nano_usd) VALUES(true,1000000000);
CREATE TABLE credits.accounts (
 payer text PRIMARY KEY,
 monthly_nano_usd bigint CHECK(monthly_nano_usd>=0),
 unlimited boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0)
);
CREATE TABLE credits.policy_changes (
 actor text NOT NULL,
 operation_key text NOT NULL,
 request jsonb NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor,operation_key)
);
ALTER TABLE credits.attempts
 ADD COLUMN period_start date,
 ADD COLUMN enforced boolean NOT NULL DEFAULT false,
 ADD COLUMN request_sha256 text;
UPDATE credits.attempts SET period_start=date_trunc('month',created_at AT TIME ZONE 'UTC')::date;
ALTER TABLE credits.attempts ALTER COLUMN period_start SET NOT NULL;
ALTER TABLE credits.attempts ALTER COLUMN period_start SET DEFAULT date_trunc('month',clock_timestamp() AT TIME ZONE 'UTC')::date;
CREATE INDEX credit_attempt_payer_period ON credits.attempts(payer,period_start);

DO $credits_grants$
DECLARE runtime_role text;
BEGIN
 FOREACH runtime_role IN ARRAY ARRAY['zns_app','zns_api','zns_bot','zns_runtime','zns_meter'] LOOP
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=runtime_role) THEN
   EXECUTE format('GRANT USAGE ON SCHEMA credits,core TO %I',runtime_role);
   EXECUTE format('GRANT SELECT ON core.pass_booking_admins TO %I',runtime_role);
   EXECUTE format('GRANT SELECT,INSERT,UPDATE ON credits.attempts TO %I',runtime_role);
   EXECUTE format('GRANT SELECT,INSERT(payer),UPDATE(version) ON credits.accounts TO %I',runtime_role);
   EXECUTE format('GRANT SELECT ON credits.default_policy,credits.price_versions,credits.price_selection TO %I',runtime_role);
  END IF;
 END LOOP;
 FOREACH runtime_role IN ARRAY ARRAY['zns_app','zns_api','zns_runtime'] LOOP
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=runtime_role) THEN
   EXECUTE format('GRANT UPDATE ON credits.default_policy TO %I',runtime_role);
   EXECUTE format('GRANT UPDATE(monthly_nano_usd,unlimited) ON credits.accounts TO %I',runtime_role);
   EXECUTE format('GRANT SELECT,INSERT ON credits.policy_changes TO %I',runtime_role);
  END IF;
 END LOOP;
END
$credits_grants$;
