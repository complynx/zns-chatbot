-- name: InsertIngress :exec
INSERT INTO core.registration_ingress(kind,bot_id,request_key,telegram_id,native_event,native_owner,native_payload,received_at)
VALUES('telegram',sqlc.arg(bot_id)::bigint,sqlc.arg(request_key)::text,sqlc.arg(sender)::bigint,
 sqlc.narg(native_event)::text,sqlc.narg(native_owner)::text,sqlc.narg(native_payload)::jsonb,
 COALESCE(sqlc.narg(received_at)::timestamptz,clock_timestamp()))
ON CONFLICT DO NOTHING;
