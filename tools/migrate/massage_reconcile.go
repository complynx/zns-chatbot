package migrate

// Only the captured notice set belongs to import reconciliation. Runtime may add
// reminders and advance additional delivery; immutable intent and queue identity
// stay exact. Historical sent-marker rows permit no delivery progress.
const massageSnapshotSQL = `SELECT jsonb_build_object(
 'drafts',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.legacy_massage_drafts t WHERE event_id=ANY($1::text[])),
 'deferred',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_user_deferred_domains t WHERE domain='massage' AND source_key IN(SELECT source_key FROM core.legacy_massage_import_references WHERE bot_id=$4)),
 'events',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_events t WHERE id=ANY($1::text[])),
 'parties',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_parties t WHERE event_id=ANY($1::text[])),
 'specialists',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY event_id,owner),'[]') FROM core.massage_specialists t WHERE event_id=ANY($1::text[])),
 'work',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_work t WHERE event_id=ANY($1::text[])),
 'bookings',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY id),'[]') FROM core.massage_bookings t WHERE event_id=ANY($1::text[])),
 'notices',(SELECT COALESCE(jsonb_agg(CASE WHEN t.kind='additional' THEN
  jsonb_build_object('id',t.id,'booking_id',t.booking_id,'owner',t.owner,'kind',t.kind,
   'created_at',t.created_at,'bot_id',t.bot_id,'delivery_chat',t.delivery_chat)
  ELSE to_jsonb(t) END ORDER BY t.id),'[]') FROM core.massage_notices t WHERE t.id=ANY($5::bigint[])),
 'notice_bindings',(SELECT COALESCE(jsonb_agg(jsonb_build_object(
  'bot_id',q.bot_id,'owner_kind',q.owner_kind,'owner_key',q.owner_key,'effect_key',q.effect_key,
  'chat',q.chat,'thread_id',q.thread_id,'lane_sequence',q.lane_sequence,'traffic_class',q.traffic_class)
  ORDER BY q.bot_id,q.owner_key,q.effect_key),'[]') FROM core.delivery_queue q
  WHERE q.owner_kind='massage' AND q.owner_key IN(SELECT id::text FROM core.massage_notices WHERE id=ANY($5::bigint[]))),
 'references',(SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY source_key),'[]') FROM core.legacy_massage_import_references t WHERE bot_id=$4))`
