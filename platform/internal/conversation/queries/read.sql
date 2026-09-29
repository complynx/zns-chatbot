-- name: KnownActor :one
SELECT EXISTS(SELECT 1 FROM core.users WHERE id = $1);

-- name: ReadEvents :many
SELECT id, kind, text, details, omitted, created_at, EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id) AS has_full_text, omission_reason
FROM core.conversation_events
WHERE owner = sqlc.arg(owner)
  AND (sqlc.arg(before_id)::bigint = 0 OR id < sqlc.arg(before_id))
  AND id > sqlc.arg(after_id)
ORDER BY id DESC
LIMIT sqlc.arg(page_limit)::bigint;

-- name: RecentEvents :many
SELECT id, kind, text, details, omitted, created_at, has_full_text, omission_reason FROM (
  SELECT id, kind, text, details, omitted, created_at, EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id) AS has_full_text, omission_reason
  FROM core.conversation_events WHERE owner = sqlc.arg(owner)
  ORDER BY id DESC LIMIT sqlc.arg(recent_limit)::bigint
) recent ORDER BY id;

-- name: ReadSummary :one
SELECT version, through_id, text FROM core.conversation_summaries WHERE owner = $1;

-- name: HasSummaryGap :one
SELECT EXISTS(SELECT 1 FROM core.conversation_events
  WHERE owner = $1 AND id < $2 AND summarized_version = 0);

-- name: SummaryEvents :many
SELECT id, kind, text, details, omitted, created_at, EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id) AS has_full_text, omission_reason
FROM core.conversation_events
WHERE owner = sqlc.arg(owner) AND id < sqlc.arg(before_id) AND summarized_version = 0
 AND NOT EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id)
ORDER BY id LIMIT sqlc.arg(page_limit)::bigint;

-- name: HistoryGeneration :one
SELECT COALESCE((SELECT generation FROM core.conversation_history_generations WHERE owner=$1),0)::bigint AS generation;

-- name: ReadText :one
SELECT e.id, e.omitted,
 CASE WHEN e.omitted THEN '' ELSE substring(COALESCE(b.body,e.text) FROM sqlc.arg(character_offset)::integer+1 FOR sqlc.arg(character_limit)::integer) END::text AS text,
 CASE WHEN e.omitted THEN '' ELSE COALESCE(b.body_sha256,encode(sha256(convert_to(e.text,'UTF8')),'hex')) END::text AS digest,
 CASE WHEN e.omitted THEN 0 ELSE COALESCE(b.character_count,char_length(e.text)) END::integer AS total,
 COALESCE((SELECT generation FROM core.conversation_history_generations WHERE owner=e.owner),0)::bigint AS generation
FROM core.conversation_events e LEFT JOIN core.conversation_message_bodies b ON b.event_id=e.id
WHERE e.owner=sqlc.arg(owner) AND e.id=sqlc.arg(event_id);


