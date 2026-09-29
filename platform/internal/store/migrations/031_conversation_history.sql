CREATE TABLE core.conversation_events (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner text NOT NULL REFERENCES core.users(id), source_key text NOT NULL,
 kind text NOT NULL CHECK(kind IN ('user','assistant','manual','domain','system')),
 text text NOT NULL DEFAULT '' CHECK(octet_length(text)<=5000),
 details jsonb NOT NULL DEFAULT '{}', omitted boolean NOT NULL DEFAULT false,
 summarized_version bigint NOT NULL DEFAULT 0,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), UNIQUE(owner,source_key)
);
CREATE INDEX conversation_owner_id ON core.conversation_events(owner,id);
CREATE TABLE core.conversation_summaries (
 owner text PRIMARY KEY REFERENCES core.users(id), version bigint NOT NULL DEFAULT 0,
 through_id bigint NOT NULL DEFAULT 0, text text NOT NULL DEFAULT '' CHECK(octet_length(text)<=2048)
);

-- Per-event summary coverage handles late commits below a numeric checkpoint.
-- Domain triggers do not acquire extra locks across multiple booking owners.
CREATE FUNCTION core.conversation_audit() RETURNS trigger LANGUAGE plpgsql AS $$
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
 INSERT INTO core.conversation_events(owner,source_key,kind,details)
 VALUES(subject_owner,source,'domain',payload) ON CONFLICT(owner,source_key) DO NOTHING;
 RETURN NEW;
END $$;
CREATE TRIGGER conversation_workflow AFTER INSERT ON core.audit FOR EACH ROW EXECUTE FUNCTION core.conversation_audit();
CREATE TRIGGER conversation_order AFTER INSERT ON core.order_audit FOR EACH ROW EXECUTE FUNCTION core.conversation_audit();
CREATE TRIGGER conversation_profile AFTER INSERT ON core.pass_profile_history FOR EACH ROW EXECUTE FUNCTION core.conversation_audit();
CREATE TRIGGER conversation_knowledge AFTER INSERT ON core.knowledge_audit FOR EACH ROW EXECUTE FUNCTION core.conversation_audit();
CREATE TRIGGER conversation_pass AFTER INSERT OR UPDATE ON core.pass_bookings FOR EACH ROW EXECUTE FUNCTION core.conversation_audit();
CREATE TRIGGER conversation_massage AFTER INSERT OR UPDATE ON core.massage_bookings FOR EACH ROW EXECUTE FUNCTION core.conversation_audit();
