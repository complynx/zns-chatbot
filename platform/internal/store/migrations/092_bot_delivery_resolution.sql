-- Keep original admitted continuation and correlation before crossing the wire.
-- Older attempts have no trustworthy capture and remain unresolved.
CREATE TABLE bot.delivery_attempts (
 bot_id bigint NOT NULL,
 operation_key text NOT NULL,
 effect_key text NOT NULL,
 attempt bigint NOT NULL CHECK(attempt>0),
 method text NOT NULL CHECK(method IN ('sendMessage','editMessageText','sendDocument')),
 payload_sha256 text NOT NULL CHECK(payload_sha256 ~ '^[0-9a-f]{64}$'),
 continuation jsonb NOT NULL,
 PRIMARY KEY(bot_id,operation_key,effect_key,attempt),
 FOREIGN KEY(bot_id,operation_key,effect_key)
 REFERENCES bot.delivery_intents(bot_id,operation_key,effect_key)
);
CREATE TABLE bot.delivery_resolutions (
 actor text NOT NULL,
 operation_key text NOT NULL CHECK(length(operation_key) BETWEEN 1 AND 128),
 request jsonb NOT NULL,
 result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor,operation_key)
);
DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime','zns_bot') LOOP
  EXECUTE format('REVOKE UPDATE,DELETE ON bot.delivery_attempts FROM %I',runtime_role);
  EXECUTE format('GRANT SELECT,INSERT ON bot.delivery_attempts TO %I',runtime_role);
 END LOOP;
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='zns_bot') THEN
  REVOKE ALL ON bot.delivery_resolutions FROM zns_bot;
 END IF;
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime') LOOP
  EXECUTE format('REVOKE UPDATE,DELETE ON bot.delivery_resolutions FROM %I',runtime_role);
  EXECUTE format('GRANT SELECT,INSERT ON bot.delivery_resolutions TO %I',runtime_role);
 END LOOP;
END $$;
