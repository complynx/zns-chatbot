-- name: DeliveryClock :one
SELECT clock_timestamp()::timestamptz AS now;

-- name: EnsurePacing :exec
INSERT INTO core.delivery_pacing(bot_id,chat) VALUES(sqlc.arg(bot_id)::bigint,sqlc.arg(chat)::text)
ON CONFLICT DO NOTHING;

-- name: LockPacing :one
SELECT not_before,pause_reason FROM core.delivery_pacing
WHERE bot_id=sqlc.arg(bot_id)::bigint AND chat=sqlc.arg(chat)::text FOR UPDATE;

-- name: ExtendPacing :exec
UPDATE core.delivery_pacing SET not_before=GREATEST(not_before,sqlc.arg(not_before)::timestamptz),
 pause_reason=CASE WHEN sqlc.arg(pause_reason)::text<>'' THEN sqlc.arg(pause_reason)::text ELSE pause_reason END
WHERE bot_id=sqlc.arg(bot_id)::bigint AND chat=sqlc.arg(chat)::text;
