-- name: NativeBindings :many
SELECT b.action,v.state,v.revision,v.owner
FROM bot.pass_views v JOIN bot.pass_buttons b ON b.owner=v.owner AND b.revision=v.revision
WHERE v.chat_id=sqlc.arg(chat_id)::bigint AND b.token=sqlc.arg(token)::text
 AND NOT COALESCE((v.state->>'redacted')::boolean,false);

-- name: LockNativeBinding :one
SELECT b.action,v.state,v.revision,v.owner
FROM bot.pass_views v JOIN bot.pass_buttons b ON b.owner=v.owner AND b.revision=v.revision
WHERE v.owner=sqlc.arg(owner)::text AND v.chat_id=sqlc.arg(chat_id)::bigint AND b.token=sqlc.arg(token)::text
 AND NOT COALESCE((v.state->>'redacted')::boolean,false)
FOR SHARE OF v,b;

-- name: PendingNativeCandidates :many
SELECT id,bot_id,request_key,telegram_id,received_at,native_payload
FROM core.registration_ingress WHERE native_event=sqlc.arg(event)::text
 AND native_payload IS NOT NULL AND native_outcome=''
ORDER BY id;

-- name: CompleteNativeCandidate :exec
UPDATE core.registration_ingress SET native_outcome=sqlc.arg(outcome)::text,
 native_intent_id=sqlc.narg(intent_id)::bigint
WHERE id=sqlc.arg(id)::bigint AND native_outcome='' AND native_payload=sqlc.arg(payload)::jsonb;

-- name: NativePendingEvents :many
SELECT DISTINCT native_event::text AS event FROM core.registration_ingress
WHERE native_payload IS NOT NULL AND native_outcome='' ORDER BY native_event;
