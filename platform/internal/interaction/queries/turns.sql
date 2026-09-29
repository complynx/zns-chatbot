-- name: LoadTurn :one
SELECT * FROM interaction.saved_turns WHERE owner=$1 AND update_id=$2;

-- name: SaveTurnWinner :one
INSERT INTO interaction.saved_turns(owner,update_id,payload,kind,state,reason,history_generation)
VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(owner,update_id) DO UPDATE SET payload=interaction.saved_turns.payload
RETURNING *;

-- name: MarkTerminal :execrows
INSERT INTO interaction.saved_turns(owner,update_id,payload,kind,state,reason,history_generation)
VALUES($1,$2,$3,'terminal','privacy_terminal',$4,$5) ON CONFLICT(owner,update_id) DO UPDATE
SET payload=excluded.payload,kind=excluded.kind,state=excluded.state,reason=excluded.reason,history_generation=excluded.history_generation
WHERE interaction.saved_turns.payload->'format_version'='1'::jsonb;

-- name: DeleteDerivedReplies :exec
DELETE FROM bot.interactions AS target WHERE target.owner=$1 AND target.update_id=$2
AND target.kind IN ('reply','orders_reply','knowledge_reply','profile_reply','profile_answer','registration_reply')
AND NOT EXISTS(SELECT 1 FROM bot.interactions origin WHERE origin.owner=$1 AND origin.update_id=$2
 AND origin.kind='reply_origin' AND origin.content='"authoritative"'::jsonb);

-- name: ClearDerivedReplies :exec
UPDATE bot.interactions AS target SET content='""'::jsonb,native_markdown=false
WHERE target.owner=$1 AND target.update_id=$2 AND target.kind IN ('reply','orders_reply','knowledge_reply','profile_answer','registration_reply')
AND NOT EXISTS(SELECT 1 FROM bot.interactions origin WHERE origin.owner=$1 AND origin.update_id=$2
 AND origin.kind='reply_origin' AND origin.content='"authoritative"'::jsonb);

-- name: InsertTerminalReply :exec
INSERT INTO bot.interactions(owner,update_id,kind,content)
VALUES($1,$2,'reply','""'::jsonb) ON CONFLICT DO NOTHING;

-- name: ReplyOrigin :one
SELECT (content#>>'{}')::text AS origin FROM bot.interactions WHERE owner=$1 AND update_id=$2 AND kind='reply_origin';

-- name: LatestNotice :one
SELECT i.content,i.native_markdown,i.update_id,t.payload,
COALESCE(t.kind,'')::text AS kind, COALESCE(t.state,'')::text AS state,
COALESCE(t.reason,'')::text AS reason, COALESCE(t.history_generation,0)::bigint AS history_generation,
EXISTS(SELECT 1 FROM bot.interactions origin WHERE origin.owner=i.owner AND origin.update_id=i.update_id
 AND origin.kind='reply_origin' AND origin.content='"authoritative"'::jsonb)::boolean AS trusted
FROM bot.interactions i LEFT JOIN interaction.saved_turns t ON t.owner=i.owner AND t.update_id=i.update_id
WHERE i.owner=$1 AND i.kind='reply' ORDER BY i.id DESC LIMIT 1;

-- name: ConsumedVoice :one
SELECT r.payload,r.kind,r.state,r.reason,r.history_generation,
EXISTS(SELECT 1 FROM bot.media_intake m
WHERE m.owner=r.owner AND m.id=$2 AND m.update_id=r.update_id AND m.av_kind='voice'
AND r.payload->>'media_id'=m.id AND r.payload->'av_ids' ? m.id
AND (m.status='new' OR (m.status='done' AND m.last_action='answer'))
AND (r.payload->'plan'->>'view'<>'media' OR r.payload->'plan'->'media_action'->>'media_id'<>m.id))::boolean AS consumed
FROM interaction.saved_turns r WHERE r.owner=$1 AND r.update_id=$3;
