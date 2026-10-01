-- name: InsertIngress :exec
INSERT INTO core.registration_ingress(kind,bot_id,request_key,telegram_id,native_event,native_owner,native_payload,received_at,
 intake_owner,intake_generation,intake_kind,intake_control,intake_digest,intake_message_id)
VALUES('telegram',sqlc.arg(bot_id)::bigint,sqlc.arg(request_key)::text,sqlc.arg(sender)::bigint,
 sqlc.narg(native_event)::text,sqlc.narg(native_owner)::text,sqlc.narg(native_payload)::jsonb,
 COALESCE(sqlc.narg(received_at)::timestamptz,clock_timestamp()),
 sqlc.narg(intake_owner)::text,sqlc.narg(intake_generation)::bigint,sqlc.narg(intake_kind)::text,
 sqlc.narg(intake_control)::text,sqlc.narg(intake_digest)::text,sqlc.narg(intake_message_id)::bigint)
ON CONFLICT DO NOTHING;
