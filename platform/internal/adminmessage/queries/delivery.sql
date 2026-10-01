-- name: ExpireAdminSends :exec
UPDATE core.admin_message_deliveries SET state='unknown',failure='telegram_outcome_unknown'
WHERE bot_id=sqlc.arg(bot_id)::bigint AND state='sending' AND lease_until<=clock_timestamp();

-- name: CancelUnauthorizedAdminPending :exec
UPDATE core.admin_message_deliveries d SET state='cancelled',failure='publication_cancelled'
FROM core.admin_messages m WHERE m.id=d.message_id AND d.bot_id=sqlc.arg(bot_id)::bigint
 AND d.state='pending' AND (m.state='cancelled'
 OR NOT EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=m.actor));

-- name: NextAdminDeliveries :many
SELECT d.id,d.message_id,m.actor FROM core.admin_message_deliveries d
JOIN core.admin_messages m ON m.id=d.message_id
JOIN core.pass_booking_admins a ON a.owner=m.actor
WHERE d.bot_id=sqlc.arg(bot_id)::bigint AND m.state='queued' AND d.state='pending'
 AND d.available_at<=clock_timestamp() AND (d.lease_until IS NULL OR d.lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM core.admin_message_deliveries earlier
  WHERE (earlier.bot_id=d.bot_id OR earlier.bot_id IS NULL) AND earlier.destination->>'chat'=d.destination->>'chat'
   AND COALESCE(earlier.destination->>'thread','0')=COALESCE(d.destination->>'thread','0')
   AND earlier.id<d.id AND earlier.state IN ('pending','sending','unknown','parked','paused'))
ORDER BY d.id LIMIT 25;

-- name: LockAdminDelivery :one
SELECT d.id,d.message_id,d.destination,COALESCE(d.content,m.request->'content')::jsonb AS content,
 d.state,d.attempt,m.actor,u.telegram_id
FROM core.admin_message_deliveries d JOIN core.admin_messages m ON m.id=d.message_id
JOIN core.pass_booking_admins a ON a.owner=m.actor JOIN core.users u ON u.id=m.actor
WHERE d.id=sqlc.arg(id)::bigint AND d.bot_id=sqlc.arg(bot_id)::bigint
 AND m.state='queued' AND d.state='pending' AND d.available_at<=clock_timestamp()
 AND (d.lease_until IS NULL OR d.lease_until<=clock_timestamp())
FOR UPDATE OF d,m SKIP LOCKED FOR SHARE OF a;

-- name: PrepareAdminDelivery :one
UPDATE core.admin_message_deliveries SET attempt=attempt+1,lease_until=clock_timestamp()+interval '2 minutes'
WHERE id=sqlc.arg(id)::bigint RETURNING attempt;

-- name: AdminAttemptOwner :one
SELECT m.actor,m.id FROM core.admin_message_deliveries d JOIN core.admin_messages m ON m.id=d.message_id
WHERE d.id=sqlc.arg(id)::bigint AND d.bot_id=sqlc.arg(bot_id)::bigint AND d.attempt=sqlc.arg(attempt)::bigint
 AND (d.state IN ('pending','sending','unknown') OR (d.state='cancelled' AND d.failure='source_revoked'));

-- name: LockAdminAttempt :one
SELECT d.destination,d.state,d.telegram_message_id,d.available_at,
 d.last_uncertain_attempt,d.uncertain_resends,
 COALESCE(d.lease_until>clock_timestamp(),false)::boolean AS lease_live
FROM core.admin_message_deliveries d WHERE d.id=sqlc.arg(id)::bigint
 AND d.bot_id=sqlc.arg(bot_id)::bigint AND d.attempt=sqlc.arg(attempt)::bigint
FOR UPDATE;

-- name: BeginAdminSend :execrows
UPDATE core.admin_message_deliveries SET state='sending',lease_until=clock_timestamp()+interval '2 minutes',
 uncertain_resends=uncertain_resends+CASE WHEN last_uncertain_attempt IS NOT NULL THEN 1 ELSE 0 END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempt=sqlc.arg(attempt)::bigint
 AND state='pending' AND lease_until>clock_timestamp()
 AND (last_uncertain_attempt IS NULL OR uncertain_resends<3);

-- name: RecordAdminUncertainty :exec
UPDATE core.admin_message_deliveries SET last_uncertain_attempt=sqlc.arg(attempt)::bigint,
 last_uncertain_reason=sqlc.arg(reason)::text,last_uncertain_recorded_at=clock_timestamp()
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempt=sqlc.arg(attempt)::bigint
 AND (last_uncertain_attempt IS NULL OR last_uncertain_attempt<sqlc.arg(attempt)::bigint);

-- name: FinishAdminDelivery :execrows
UPDATE core.admin_message_deliveries SET state=sqlc.arg(state)::text,
 telegram_message_id=sqlc.arg(message_id)::bigint,failure=sqlc.arg(failure)::text,
 available_at=sqlc.arg(available_at)::timestamptz,lease_until=NULL,
 failure_count=failure_count+sqlc.arg(failure_increment)::bigint
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempt=sqlc.arg(attempt)::bigint
 AND (state IN ('pending','sending','unknown') OR (state='cancelled' AND failure='source_revoked'));

-- name: EnqueueAdminDeliveries :exec
INSERT INTO core.admin_message_deliveries(message_id,destination,content,bot_id)
SELECT message_id,destination,content,CASE WHEN sqlc.arg(bot_id)::bigint>0 THEN sqlc.arg(bot_id)::bigint END
FROM core.admin_message_recipients WHERE message_id=sqlc.arg(message_id)::bigint ON CONFLICT DO NOTHING;



