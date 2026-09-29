CREATE SCHEMA IF NOT EXISTS interaction;
CREATE TABLE interaction.saved_turns (
 owner text NOT NULL REFERENCES core.users(id),
 update_id bigint NOT NULL,
 kind text NOT NULL CHECK(kind IN ('derived','command','notice','terminal')),
 state text NOT NULL CHECK(state IN ('ready','privacy_terminal')),
 reason text NOT NULL DEFAULT '' CHECK(reason IN ('','history_deleted','source_revoked')),
 history_generation bigint NOT NULL CHECK(history_generation>=0),
 payload jsonb NOT NULL CHECK(jsonb_typeof(payload)='object'),
 PRIMARY KEY(owner,update_id),
 CHECK((state='ready' AND kind<>'terminal' AND reason='') OR
       (state='privacy_terminal' AND kind='terminal' AND reason<>''))
);

ALTER TABLE core.conversation_events ADD COLUMN origin text NOT NULL DEFAULT 'original'
 CHECK(origin IN ('original','derived','trusted'));

-- Explicit origin survives missing turn state and is independent of message role.
CREATE OR REPLACE VIEW core.conversation_unproven_runtime AS
SELECT e.id,e.owner,e.summarized_version FROM core.conversation_events e
WHERE e.origin='derived' AND NOT e.omitted
AND NOT EXISTS(SELECT 1 FROM core.conversation_read_authorities a WHERE a.event_id=e.id);

-- Unreleased Go plans have no upgrade contract. Do not guess their owner or origin.
DROP TABLE bot.replies;

-- Preserve domain audit behavior; only mark its explicit trusted origin.
CREATE OR REPLACE FUNCTION core.conversation_audit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE subject_owner text; source text; payload jsonb;
BEGIN
 IF TG_TABLE_NAME='audit' THEN
  subject_owner:=NEW.owner; source:='workflow:'||NEW.id;
  payload:=jsonb_build_object('domain','workflow','action',NEW.action,'version',NEW.version);
 ELSIF TG_TABLE_NAME='order_audit' THEN
  SELECT owner INTO subject_owner FROM core.orders WHERE id=NEW.order_id;
  source:='order:'||NEW.id;
  payload:=jsonb_build_object('domain','order','action',NEW.action,'object',NEW.order_id,'version',NEW.version,'state',NEW.snapshot->>'state');
 ELSIF TG_TABLE_NAME='pass_profile_history' THEN
  subject_owner:=NEW.owner; source:='profile:'||NEW.version;
  payload:=jsonb_build_object('domain','profile','action',NEW.action,'field',NEW.field,'version',NEW.version);
 ELSIF TG_TABLE_NAME='knowledge_audit' THEN
  subject_owner:=NEW.actor; source:='knowledge:'||NEW.id;
  payload:=jsonb_build_object('domain','knowledge','action',NEW.action,'object',NEW.subject,'version',NEW.version);
 ELSIF TG_TABLE_NAME='pass_bookings' THEN
  IF TG_OP='UPDATE' AND NEW.version=OLD.version THEN RETURN NEW; END IF;
  subject_owner:=NEW.owner; source:='pass:'||NEW.event_id||':'||NEW.version;
  payload:=jsonb_build_object('domain','pass','action','state_changed','object',NEW.event_id,'version',NEW.version,'state',NEW.state);
 ELSIF TG_TABLE_NAME='massage_bookings' THEN
  IF TG_OP='UPDATE' AND NEW.version=OLD.version THEN RETURN NEW; END IF;
  subject_owner:=NEW.owner; source:='massage:'||NEW.id||':'||NEW.version;
  payload:=jsonb_build_object('domain','massage','action','state_changed','object',NEW.id,'version',NEW.version,
   'state',CASE WHEN NEW.cancelled_at IS NULL THEN 'booked' ELSE 'cancelled' END);
 END IF;
 INSERT INTO core.conversation_events(owner,source_key,kind,details,origin)
 VALUES(subject_owner,source,'domain',payload,'trusted') ON CONFLICT(owner,source_key) DO NOTHING;
 RETURN NEW;
END $$;
