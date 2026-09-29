CREATE TABLE credits.operator_changes (
 actor text NOT NULL,
 operation_key text NOT NULL,
 request jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor,operation_key)
);
CREATE TABLE credits.reconciliations (
 attempt_id uuid PRIMARY KEY REFERENCES credits.attempts(id),
 cost_nano_usd bigint NOT NULL CHECK(cost_nano_usd>=0),
 evidence text NOT NULL,
 actor text NOT NULL,
 operation_key text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(actor,operation_key) REFERENCES credits.operator_changes(actor,operation_key)
);
CREATE TABLE credits.adjustments (
 actor text NOT NULL,
 operation_key text NOT NULL,
 payer text NOT NULL,
 period_start date NOT NULL,
 delta_nano_usd bigint NOT NULL,
 evidence text NOT NULL,
 PRIMARY KEY(actor,operation_key),
 FOREIGN KEY(actor,operation_key) REFERENCES credits.operator_changes(actor,operation_key)
);
CREATE TABLE credits.cutover (
 singleton boolean PRIMARY KEY DEFAULT true CHECK(singleton),
 epoch timestamptz NOT NULL,
 actor text NOT NULL,
 operation_key text NOT NULL,
 FOREIGN KEY(actor,operation_key) REFERENCES credits.operator_changes(actor,operation_key)
);
CREATE TABLE bot.budget_operations (
 owner text NOT NULL,
 update_id bigint NOT NULL,
 mode text NOT NULL CHECK(mode IN ('legacy','credits')),
 cutover_epoch timestamptz,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner,update_id)
);
DO $grants$
DECLARE runtime_role text;
BEGIN
 FOREACH runtime_role IN ARRAY ARRAY['zns_app','zns_api','zns_bot','zns_runtime','zns_meter'] LOOP
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=runtime_role) THEN
   EXECUTE format('GRANT SELECT ON credits.reconciliations,credits.adjustments,credits.cutover TO %I',runtime_role);
  END IF;
 END LOOP;
 FOREACH runtime_role IN ARRAY ARRAY['zns_app','zns_api','zns_runtime'] LOOP
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=runtime_role) THEN
   EXECUTE format('GRANT SELECT,INSERT ON credits.operator_changes,credits.reconciliations,credits.adjustments,credits.cutover TO %I',runtime_role);
   EXECUTE format('GRANT SELECT,INSERT,UPDATE(version) ON credits.price_versions TO %I',runtime_role);
   EXECUTE format('GRANT SELECT,INSERT,UPDATE ON credits.price_selection TO %I',runtime_role);
  END IF;
 END LOOP;
 FOREACH runtime_role IN ARRAY ARRAY['zns_app','zns_bot','zns_runtime'] LOOP
  IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname=runtime_role) THEN
   EXECUTE format('GRANT SELECT,INSERT ON bot.budget_operations TO %I',runtime_role);
  END IF;
 END LOOP;
END
$grants$;
