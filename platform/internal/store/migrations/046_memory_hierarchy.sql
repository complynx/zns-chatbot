-- Capture only approved shared facts and owner-private records. Pending
-- suggestions never enter this evidence history.
CREATE TABLE core.memory_revisions (
 namespace text NOT NULL CHECK(namespace IN ('shared','private')),
 owner text NOT NULL DEFAULT '', scope text NOT NULL DEFAULT '',
 topic text NOT NULL, item_key text NOT NULL, version bigint NOT NULL,
 body text NOT NULL, active boolean NOT NULL,
 captured_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(namespace,owner,scope,topic,item_key,version)
);
INSERT INTO core.memory_revisions(namespace,scope,topic,item_key,version,body,active)
 SELECT 'shared',scope,topic,fact_key,version,body,active FROM core.knowledge_facts;
INSERT INTO core.memory_revisions(namespace,owner,topic,item_key,version,body,active)
 SELECT 'private',owner,CASE WHEN position('.' IN memo_key)>1 THEN split_part(memo_key,'.',1) ELSE 'notes' END,memo_key,version,body,active
 FROM core.knowledge_memos;
CREATE FUNCTION core.capture_memory_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ns text; who text; sc text; tp text; ky text;
BEGIN
 IF TG_TABLE_NAME='knowledge_facts' THEN
  ns := 'shared'; who := ''; sc := NEW.scope; tp := NEW.topic; ky := NEW.fact_key;
 ELSE
  ns := 'private'; who := NEW.owner; sc := '';
  tp := CASE WHEN position('.' IN NEW.memo_key)>1 THEN split_part(NEW.memo_key,'.',1) ELSE 'notes' END;
  ky := NEW.memo_key;
 END IF;
 IF NOT NEW.active THEN
  UPDATE core.memory_revisions SET body='' WHERE namespace=ns AND owner=who AND scope=sc AND topic=tp AND item_key=ky;
 END IF;
 INSERT INTO core.memory_revisions(namespace,owner,scope,topic,item_key,version,body,active)
 VALUES(ns,who,sc,tp,ky,NEW.version,CASE WHEN NEW.active THEN NEW.body ELSE '' END,NEW.active);
 RETURN NEW;
END $$;
CREATE TRIGGER memory_fact_revision AFTER INSERT OR UPDATE ON core.knowledge_facts
 FOR EACH ROW EXECUTE FUNCTION core.capture_memory_revision();
CREATE TRIGGER memory_memo_revision AFTER INSERT OR UPDATE ON core.knowledge_memos
 FOR EACH ROW EXECUTE FUNCTION core.capture_memory_revision();
