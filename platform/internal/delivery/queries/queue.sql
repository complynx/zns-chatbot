-- name: EnsureDeliveryLane :exec
INSERT INTO core.delivery_lanes(bot_id,chat) VALUES(sqlc.arg(bot_id)::bigint,sqlc.arg(chat)::text)
ON CONFLICT DO NOTHING;

-- name: LockDeliveryLane :one
SELECT next_sequence FROM core.delivery_lanes
WHERE bot_id=sqlc.arg(bot_id)::bigint AND chat=sqlc.arg(chat)::text FOR UPDATE;

-- name: AllocateDeliverySequence :one
UPDATE core.delivery_lanes SET next_sequence=next_sequence+1
WHERE bot_id=sqlc.arg(bot_id)::bigint AND chat=sqlc.arg(chat)::text RETURNING next_sequence;

-- name: InsertDeliveryEntry :exec
INSERT INTO core.delivery_queue(bot_id,owner_kind,owner_key,effect_key,chat,thread_id,lane_sequence,traffic_class)
VALUES(sqlc.arg(bot_id)::bigint,sqlc.arg(owner_kind)::text,sqlc.arg(owner_key)::text,sqlc.arg(effect_key)::text,
 sqlc.arg(chat)::text,sqlc.arg(thread_id)::bigint,sqlc.arg(lane_sequence)::bigint,sqlc.arg(traffic_class)::text)
ON CONFLICT(bot_id,owner_kind,owner_key,effect_key) DO NOTHING;

-- name: ReadDeliveryEntry :one
SELECT * FROM core.delivery_queue WHERE bot_id=sqlc.arg(bot_id)::bigint
AND owner_kind=sqlc.arg(owner_kind)::text AND owner_key=sqlc.arg(owner_key)::text AND effect_key=sqlc.arg(effect_key)::text;

-- name: LockDeliveryEntry :one
SELECT * FROM core.delivery_queue WHERE bot_id=sqlc.arg(bot_id)::bigint
AND owner_kind=sqlc.arg(owner_kind)::text AND owner_key=sqlc.arg(owner_key)::text AND effect_key=sqlc.arg(effect_key)::text
FOR UPDATE;

-- name: IsDeliveryHead :one
SELECT NOT EXISTS(SELECT 1 FROM core.delivery_queue
 WHERE bot_id=sqlc.arg(bot_id)::bigint AND chat=sqlc.arg(chat)::text AND lane_sequence<sqlc.arg(lane_sequence)::bigint
 AND state IN ('pending','sending','unknown','parked','paused'))::boolean AS head;

-- name: ProjectDeliveryEntry :execrows
UPDATE core.delivery_queue SET state=sqlc.arg(state)::text,not_before=sqlc.arg(not_before)::timestamptz
WHERE bot_id=sqlc.arg(bot_id)::bigint AND owner_kind=sqlc.arg(owner_kind)::text
AND owner_key=sqlc.arg(owner_key)::text AND effect_key=sqlc.arg(effect_key)::text;

-- name: AdvanceDeliveryFairness :one
INSERT INTO core.delivery_fairness(bot_id,grants) VALUES(sqlc.arg(bot_id)::bigint,1)
ON CONFLICT(bot_id) DO UPDATE SET grants=core.delivery_fairness.grants+1 RETURNING grants;

-- name: MarkDeliveryLaneServed :exec
UPDATE core.delivery_lanes SET last_served=sqlc.arg(grants)::bigint
WHERE bot_id=sqlc.arg(bot_id)::bigint AND chat=sqlc.arg(chat)::text;

-- name: DeliveryCandidates :many
WITH heads AS (
 SELECT DISTINCT ON (bot_id,chat) * FROM core.delivery_queue
 WHERE bot_id=sqlc.arg(bot_id)::bigint AND state IN ('pending','sending','unknown','parked','paused')
 ORDER BY bot_id,chat,lane_sequence
)
SELECT h.* FROM heads h JOIN core.delivery_lanes l USING(bot_id,chat)
LEFT JOIN core.delivery_fairness f USING(bot_id)
LEFT JOIN core.delivery_pacing b ON b.bot_id=h.bot_id AND b.chat=''
LEFT JOIN core.delivery_pacing c ON c.bot_id=h.bot_id AND c.chat=h.chat
WHERE h.state='pending' AND h.not_before<=clock_timestamp()
AND COALESCE(b.pause_reason,'')='' AND COALESCE(c.pause_reason,'')=''
AND COALESCE(b.not_before,'-infinity'::timestamptz)<=clock_timestamp()
AND COALESCE(c.not_before,'-infinity'::timestamptz)<=clock_timestamp()
ORDER BY (h.traffic_class <> CASE WHEN COALESCE(f.grants,0)%4=3 THEN 'background' ELSE 'interactive' END),
 l.last_served,h.chat,h.lane_sequence
LIMIT sqlc.arg(maximum)::integer;
