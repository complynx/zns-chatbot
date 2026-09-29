ALTER TABLE core.registration_ingress
 ADD COLUMN native_event text,
 ADD COLUMN native_owner text,
 ADD COLUMN native_payload jsonb,
 ADD COLUMN native_outcome text NOT NULL DEFAULT '' CHECK(native_outcome IN ('','admitted','rejected')),
 ADD COLUMN native_intent_id bigint;
ALTER TABLE core.registration_ingress ADD CONSTRAINT registration_native_envelope_complete CHECK (
 (native_payload IS NULL AND native_event IS NULL AND native_owner IS NULL) OR
 COALESCE(kind='telegram' AND jsonb_typeof(native_payload)='object' AND native_event<>'' AND native_owner<>'' AND native_payload->>'owner'=native_owner AND native_payload->'command'->>'event'=native_event,false)
);
CREATE INDEX registration_native_pending ON core.registration_ingress(native_event,id)
 WHERE native_payload IS NOT NULL AND native_outcome='';
-- No event/owner foreign keys: inbox intake must not acquire domain row locks.
DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime') LOOP
  EXECUTE format('GRANT USAGE ON SCHEMA bot TO %I',runtime_role);
  EXECUTE format('GRANT SELECT ON bot.pass_views,bot.pass_buttons TO %I',runtime_role);
  -- PostgreSQL requires UPDATE on one column for SELECT FOR SHARE.
  EXECUTE format('GRANT UPDATE(revision) ON bot.pass_views,bot.pass_buttons TO %I',runtime_role);
 END LOOP;

END $$;

-- Processing may append a resolution, but never rewrite received evidence.
CREATE FUNCTION core.registration_native_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.native_event IS DISTINCT FROM NEW.native_event OR OLD.native_owner IS DISTINCT FROM NEW.native_owner
 OR OLD.native_payload IS DISTINCT FROM NEW.native_payload THEN
  RAISE EXCEPTION 'native registration evidence is immutable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER registration_native_immutable BEFORE UPDATE ON core.registration_ingress
 FOR EACH ROW EXECUTE FUNCTION core.registration_native_immutable();
