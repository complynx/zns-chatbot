-- Keep idempotency identities and mutation metadata, but not deleted bodies.
CREATE TABLE core.memory_deletion_epochs (
 owner text PRIMARY KEY, generation bigint NOT NULL CHECK(generation>0)
);

CREATE FUNCTION core.scrub_memory_receipts(who text, sc text, tp text, ky text, sk text, deleted_version bigint)
RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 UPDATE core.knowledge_operations SET result=
  jsonb_set(result,ARRAY[sk,'text'],'""'::jsonb) || '{"redacted":true}'::jsonb
 WHERE result ? sk AND (sk='fact' OR actor=who)
  AND result->sk->>'key'=ky
  AND (sk='memo' OR result->sk->>'topic'=tp)
  AND (sk<>'fact' OR result->sk->>'event'=sc)
  AND (result->sk->>'version')::bigint < deleted_version;
 IF sk='fact' THEN
  UPDATE core.knowledge_operations o SET result=
   jsonb_set(o.result,'{proposal,text}','""'::jsonb) || '{"redacted":true}'::jsonb
  FROM core.knowledge_proposals p
  WHERE p.scope=sc AND p.topic=tp AND p.fact_key=ky AND p.state='approved'
   AND p.fact_version+1 < deleted_version
   AND o.result->'proposal'->>'id'=p.id::text;
  UPDATE core.knowledge_proposals SET body=''
  WHERE scope=sc AND topic=tp AND fact_key=ky AND state='approved'
   AND fact_version+1 < deleted_version;
 END IF;
END $$;

CREATE FUNCTION core.delete_memory_receipts() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE who text; sc text; tp text; ky text; sk text;
BEGIN
 IF NEW.active THEN RETURN NEW; END IF;
 IF TG_TABLE_NAME='knowledge_facts' THEN
  who:=''; sc:=NEW.scope; tp:=NEW.topic; ky:=NEW.fact_key; sk:='fact';
 ELSIF TG_TABLE_NAME='memory_documents' THEN
  who:=NEW.owner; sc:=''; tp:=NEW.topic; ky:=NEW.document_key; sk:='document';
 ELSE
  who:=NEW.owner; sc:=''; tp:=''; ky:=NEW.memo_key; sk:='memo';
 END IF;
 PERFORM core.scrub_memory_receipts(who,sc,tp,ky,sk,NEW.version);
 INSERT INTO core.memory_deletion_epochs(owner,generation) VALUES(who,1)
 ON CONFLICT(owner) DO UPDATE SET generation=core.memory_deletion_epochs.generation+1;
 RETURN NEW;
END $$;
CREATE TRIGGER memory_fact_receipt_deletion AFTER INSERT OR UPDATE ON core.knowledge_facts
 FOR EACH ROW EXECUTE FUNCTION core.delete_memory_receipts();
CREATE TRIGGER memory_memo_receipt_deletion AFTER INSERT OR UPDATE ON core.knowledge_memos
 FOR EACH ROW EXECUTE FUNCTION core.delete_memory_receipts();
CREATE TRIGGER memory_document_receipt_deletion AFTER INSERT OR UPDATE ON core.memory_documents
 FOR EACH ROW EXECUTE FUNCTION core.delete_memory_receipts();

-- Revision tombstones survive later recreation, so backfill cannot erase a new
-- live version of the same key.
DO $$ DECLARE r record; BEGIN
 FOR r IN SELECT owner,scope,topic,item_key,source_kind,max(version) AS version
  FROM core.memory_revisions WHERE NOT active
  GROUP BY owner,scope,topic,item_key,source_kind
 LOOP
  PERFORM core.scrub_memory_receipts(r.owner,r.scope,r.topic,r.item_key,r.source_kind,r.version);
  INSERT INTO core.memory_deletion_epochs(owner,generation) VALUES(r.owner,1) ON CONFLICT DO NOTHING;
 END LOOP;
END $$;
