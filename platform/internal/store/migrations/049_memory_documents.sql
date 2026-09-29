CREATE TABLE core.memory_documents (
 owner text NOT NULL REFERENCES core.users(id), topic text NOT NULL, document_key text NOT NULL,
 body text NOT NULL CHECK(octet_length(body)<=64000), version bigint NOT NULL CHECK(version>0),
 active boolean NOT NULL DEFAULT true, PRIMARY KEY(owner,topic,document_key)
);
ALTER TABLE core.memory_revisions ADD COLUMN source_kind text NOT NULL DEFAULT 'fact';
UPDATE core.memory_revisions SET source_kind='memo' WHERE namespace='private';
ALTER TABLE core.memory_revisions DROP CONSTRAINT memory_revisions_pkey;
ALTER TABLE core.memory_revisions ADD PRIMARY KEY(namespace,owner,scope,topic,item_key,source_kind,version);
CREATE TABLE core.memory_sources (
 namespace text NOT NULL, owner text NOT NULL, scope text NOT NULL, topic text NOT NULL,
 item_key text NOT NULL, source_kind text NOT NULL, version bigint NOT NULL,
 source_owner text NOT NULL REFERENCES core.users(id), event_id bigint NOT NULL REFERENCES core.conversation_events(id),
 PRIMARY KEY(namespace,owner,scope,topic,item_key,source_kind,version,event_id)
);
CREATE TABLE core.memory_proposal_sources (
 proposal_id bigint NOT NULL REFERENCES core.knowledge_proposals(id),
 source_owner text NOT NULL REFERENCES core.users(id), event_id bigint NOT NULL REFERENCES core.conversation_events(id),
 PRIMARY KEY(proposal_id,event_id)
);
CREATE OR REPLACE FUNCTION core.capture_memory_revision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ns text; who text; sc text; tp text; ky text; sk text;
BEGIN
 IF TG_TABLE_NAME='knowledge_facts' THEN
  ns := 'shared'; who := ''; sc := NEW.scope; tp := NEW.topic; ky := NEW.fact_key; sk := 'fact';
 ELSIF TG_TABLE_NAME='memory_documents' THEN
  ns := 'private'; who := NEW.owner; sc := ''; tp := NEW.topic; ky := NEW.document_key; sk := 'document';
 ELSE
  ns := 'private'; who := NEW.owner; sc := ''; sk := 'memo';
  tp := CASE WHEN position('.' IN NEW.memo_key)>1 THEN split_part(NEW.memo_key,'.',1) ELSE 'notes' END;
  ky := NEW.memo_key;
 END IF;
 IF NOT NEW.active THEN
  UPDATE core.memory_revisions SET body='' WHERE namespace=ns AND owner=who AND scope=sc AND topic=tp AND item_key=ky AND source_kind=sk;
  DELETE FROM core.memory_sources WHERE namespace=ns AND owner=who AND scope=sc AND topic=tp AND item_key=ky AND source_kind=sk;
 END IF;
 INSERT INTO core.memory_revisions(namespace,owner,scope,topic,item_key,source_kind,version,body,active)
 VALUES(ns,who,sc,tp,ky,sk,NEW.version,CASE WHEN NEW.active THEN NEW.body ELSE '' END,NEW.active);
 RETURN NEW;
END $$;
CREATE TRIGGER memory_document_revision AFTER INSERT OR UPDATE ON core.memory_documents
 FOR EACH ROW EXECUTE FUNCTION core.capture_memory_revision();
