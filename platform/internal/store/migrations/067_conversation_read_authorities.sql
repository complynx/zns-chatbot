-- Only trusted runtime-derived replies acquire registration provenance. Original
-- correspondence and committed domain receipts retain their existing meaning.
CREATE TABLE core.conversation_read_authorities (
 event_id bigint PRIMARY KEY REFERENCES core.conversation_events(id) ON DELETE CASCADE,
 authorities jsonb NOT NULL CHECK(jsonb_typeof(authorities)='array' AND jsonb_array_length(authorities)<=256)
);
ALTER TABLE core.conversation_summaries ADD COLUMN read_authorities jsonb NOT NULL DEFAULT '[]'
 CHECK(jsonb_typeof(read_authorities)='array' AND jsonb_array_length(read_authorities)<=256);

-- Old host-generated model replies cannot acquire authority from their text.
-- Positive runtime evidence is required; imported correspondence and independent
-- command receipts are excluded even when their kind is assistant.
CREATE VIEW core.conversation_unproven_runtime AS
SELECT e.id,e.owner,e.summarized_version FROM core.conversation_events e
WHERE e.kind='assistant' AND NOT e.omitted
AND NOT EXISTS(SELECT 1 FROM core.conversation_read_authorities a WHERE a.event_id=e.id)
AND NOT EXISTS(SELECT 1 FROM core.legacy_message_references l WHERE l.event_id=e.id)
AND NOT EXISTS(SELECT 1 FROM bot.interactions i WHERE i.owner=e.owner
 AND e.source_key='tg-assistant-' || i.update_id::text
 AND (i.kind IN ('result','profile_action','registration_action','order_error')
 OR (i.kind='reply_origin' AND i.content='"authoritative"'::jsonb)))
AND NOT EXISTS(SELECT 1 FROM bot.replies r WHERE e.source_key='tg-assistant-' || r.update_id::text
 AND (COALESCE(r.plan->>'system_notice','')<>''
 OR COALESCE(r.plan->'order_command','null')<>'null'::jsonb
 OR COALESCE(r.plan->'profile_command','null')<>'null'::jsonb
 OR COALESCE(r.plan->'knowledge_command','null')<>'null'::jsonb
 OR COALESCE(r.plan->'registration_command','null')<>'null'::jsonb
 OR COALESCE(r.plan->'registration_assignment','null')<>'null'::jsonb
 OR COALESCE(r.plan#>'{plan,action}','null')<>'null'::jsonb))
AND (EXISTS(SELECT 1 FROM bot.replies r WHERE e.source_key='tg-assistant-' || r.update_id::text)
 OR EXISTS(SELECT 1 FROM bot.interactions i WHERE i.owner=e.owner
 AND e.source_key='tg-assistant-' || i.update_id::text
 AND (i.native_markdown OR i.kind='profile_answer'
 OR (i.kind='reply_origin' AND i.content='"agent"'::jsonb)
 OR (i.kind='input' AND i.content->>'origin'='agent'))));
