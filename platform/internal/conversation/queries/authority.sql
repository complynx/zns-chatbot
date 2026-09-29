-- name: ReadAuthorities :many
SELECT a.event_id,a.authorities,a.history_generation FROM core.conversation_read_authorities a
JOIN core.conversation_events e ON e.id=a.event_id
WHERE e.owner=$1 AND e.id=ANY($2::bigint[]) AND NOT e.omitted ORDER BY a.event_id;

-- name: ReadSummaryAuthorities :one
SELECT read_authorities FROM core.conversation_summaries WHERE owner=$1;

-- name: AuthorityPageIDs :many
SELECT id FROM core.conversation_events WHERE owner=sqlc.arg(owner)
AND (sqlc.arg(before_id)::bigint=0 OR id<sqlc.arg(before_id)) AND id>sqlc.arg(after_id)::bigint
ORDER BY id DESC LIMIT sqlc.arg(page_limit)::bigint;

-- name: AuthorityBatchIDs :many
SELECT id FROM core.conversation_events WHERE owner=$1 AND id<$2 AND summarized_version=0
AND NOT EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id)
ORDER BY id LIMIT $3;

-- name: UnprovenRuntimeIDs :many
SELECT id FROM core.conversation_unproven_runtime WHERE owner=$1 AND id=ANY($2::bigint[]);

-- name: HasUnprovenRuntimeSummary :one
SELECT EXISTS(SELECT 1 FROM core.conversation_unproven_runtime WHERE owner=$1 AND summarized_version>0);

-- name: SummaryEventsByIDs :many
SELECT id,kind,text,details,omitted,created_at,
EXISTS(SELECT 1 FROM core.conversation_message_bodies b WHERE b.event_id=conversation_events.id) AS has_full_text,omission_reason
FROM core.conversation_events WHERE owner=$1 AND id=ANY($2::bigint[]) ORDER BY id;
