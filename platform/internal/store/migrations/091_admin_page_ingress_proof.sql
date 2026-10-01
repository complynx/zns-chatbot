-- Keep the first native intake generation after inbox acknowledgement.
ALTER TABLE core.registration_ingress
 ADD COLUMN intake_owner text,
 ADD COLUMN intake_generation bigint,
 ADD COLUMN intake_kind text,
 ADD COLUMN intake_control varchar(64),
 ADD COLUMN intake_digest char(64),
 ADD COLUMN intake_message_id bigint;
ALTER TABLE core.registration_ingress ADD CONSTRAINT registration_intake_complete CHECK (
 (intake_owner IS NULL AND intake_generation IS NULL AND intake_kind IS NULL AND intake_control IS NULL AND intake_digest IS NULL AND intake_message_id IS NULL)
 OR COALESCE(kind='telegram' AND intake_owner<>'' AND intake_generation>=0 AND intake_kind IN ('callback','command','message')
 AND intake_control IS NOT NULL AND intake_digest ~ '^[0-9a-f]{64}$' AND intake_message_id>=0,false)
);
CREATE OR REPLACE FUNCTION core.registration_native_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ROW(OLD.kind,OLD.bot_id,OLD.request_key,OLD.owner,OLD.telegram_id,OLD.native_event,OLD.native_owner,OLD.native_payload,
 OLD.intake_owner,OLD.intake_generation,OLD.intake_kind,OLD.intake_control,OLD.intake_digest,OLD.intake_message_id)
 IS DISTINCT FROM ROW(NEW.kind,NEW.bot_id,NEW.request_key,NEW.owner,NEW.telegram_id,NEW.native_event,NEW.native_owner,NEW.native_payload,
 NEW.intake_owner,NEW.intake_generation,NEW.intake_kind,NEW.intake_control,NEW.intake_digest,NEW.intake_message_id) THEN
  RAISE EXCEPTION 'native registration evidence is immutable';
 END IF;
 RETURN NEW;
END $$;
DO $$
BEGIN
 IF EXISTS(SELECT 1 FROM pg_roles WHERE rolname='zns_bot') THEN
  GRANT SELECT(id,telegram_id) ON core.users TO zns_bot;
  GRANT SELECT(owner,generation) ON core.conversation_history_generations TO zns_bot;
 END IF;
END $$;
