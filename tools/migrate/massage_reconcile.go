package migrate

const massageSnapshotSQL = `SELECT jsonb_build_object(
 'drafts',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.legacy_massage_drafts t WHERE event_id=ANY($1::text[])),
 'deferred',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_user_deferred_domains t WHERE domain='massage' AND source_key IN(SELECT source_key FROM core.legacy_massage_import_references WHERE bot_id=$4)),
 'events',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_events t WHERE id=ANY($1::text[])),
 'parties',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_parties t WHERE event_id=ANY($1::text[])),
 'specialists',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY event_id,owner),'[]') FROM core.massage_specialists t WHERE event_id=ANY($1::text[])),
 'work',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_work t WHERE event_id=ANY($1::text[])),
 'bookings',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_bookings t WHERE event_id=ANY($1::text[])),
 'notices',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_notices t WHERE booking_id IN(SELECT id FROM core.massage_bookings WHERE event_id=ANY($1::text[]))),
 'references',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_massage_import_references t WHERE bot_id=$4))`
