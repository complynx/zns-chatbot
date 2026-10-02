-- name: PrepareNotification :one
WITH head AS (
 SELECT n.id,COALESCE(u.telegram_id,0)::bigint AS chat FROM core.massage_notices n JOIN core.users u ON u.id=n.owner
 WHERE n.bot_id=sqlc.arg(bot_id)::bigint
 AND (sqlc.arg(id)::bigint=0 OR n.id=sqlc.arg(id)::bigint)
 AND (NOT sqlc.arg(followup_only)::boolean OR (n.delivery_state='sent' AND n.followup_pending))
 AND (sqlc.arg(owner)::text='' OR n.owner=sqlc.arg(owner)::text)
 AND (n.delivery_state='pending' OR (n.delivery_state='sent' AND n.followup_pending))
 AND n.available_at<=clock_timestamp() AND (n.lease_until IS NULL OR n.lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM core.massage_notices older
  JOIN core.delivery_queue oq ON oq.bot_id=older.bot_id AND oq.owner_kind='massage'
   AND oq.owner_key=older.id::text AND oq.effect_key='send'
  JOIN core.delivery_queue nq ON nq.bot_id=n.bot_id AND nq.owner_kind='massage'
   AND nq.owner_key=n.id::text AND nq.effect_key='send'
  WHERE older.bot_id=n.bot_id AND older.delivery_chat=n.delivery_chat
   AND older.followup_pending AND oq.lane_sequence<nq.lane_sequence)
 AND (n.delivery_state='sent' OR EXISTS(SELECT 1 FROM core.delivery_queue q
  WHERE q.bot_id=n.bot_id AND q.owner_kind='massage' AND q.owner_key=n.id::text AND q.effect_key='send'
  AND NOT EXISTS(SELECT 1 FROM core.delivery_queue previous WHERE previous.bot_id=q.bot_id
   AND previous.chat=q.chat AND previous.lane_sequence<q.lane_sequence
   AND previous.state IN ('pending','sending','unknown','parked','paused'))))
 ORDER BY n.available_at,n.id LIMIT 1 FOR UPDATE OF n SKIP LOCKED
)
UPDATE core.massage_notices n SET delivery_attempt=delivery_attempt+CASE
 WHEN delivery_state='pending' AND last_uncertain_attempt IS NOT NULL AND uncertain_resends>=3 THEN 0 ELSE 1 END,
 lease_until=clock_timestamp()+interval '2 minutes'
FROM head WHERE n.id=head.id RETURNING n.*;

-- name: ReadNotification :one
SELECT * FROM core.massage_notices WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint;

-- name: LockNotificationAttempt :one
SELECT *,COALESCE(lease_until>clock_timestamp(),false)::boolean AS lease_live FROM core.massage_notices
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint FOR UPDATE;

-- name: BeginNotificationSend :one
UPDATE core.massage_notices SET delivery_wire_payload=CASE WHEN last_uncertain_attempt IS NULL THEN sqlc.arg(wire_payload)::jsonb ELSE COALESCE(delivery_wire_payload,sqlc.arg(wire_payload)::jsonb) END,delivery_state='sending',lease_until=clock_timestamp()+interval '2 minutes',
 uncertain_resends=uncertain_resends+CASE WHEN last_uncertain_attempt IS NOT NULL THEN 1 ELSE 0 END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint
AND delivery_state='pending' AND lease_until>clock_timestamp()
AND (last_uncertain_attempt IS NULL OR uncertain_resends<3) RETURNING delivery_wire_payload;

-- name: FinishNotificationDelivery :execrows
UPDATE core.massage_notices SET delivery_state=sqlc.arg(state)::text,
 last_confirmed_attempt=CASE WHEN sqlc.arg(confirmed)::boolean THEN delivery_attempt ELSE last_confirmed_attempt END,
 telegram_message_id=sqlc.arg(message_id)::bigint,delivery_text=sqlc.arg(text)::text,failure=sqlc.arg(failure)::text,
 available_at=CASE WHEN last_uncertain_attempt IS NOT NULL AND sqlc.arg(state)::text='pending'
  THEN GREATEST(available_at,sqlc.arg(available_at)::timestamptz) ELSE sqlc.arg(available_at)::timestamptz END,
 lease_until=CASE WHEN sqlc.arg(state)::text='sent' THEN clock_timestamp()+interval '2 minutes' END,
 followup_pending=(sqlc.arg(state)::text='sent'),
 failure_count=failure_count+sqlc.arg(failure_increment)::bigint,
 sent_at=CASE WHEN sqlc.arg(state)::text IN ('failed','cancelled') THEN clock_timestamp() END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint
AND delivery_state IN ('pending','sending','unknown');

-- name: FinishNotificationFollowup :execrows
UPDATE core.massage_notices SET followup_pending=NOT sqlc.arg(done)::boolean,
 followup_failure=sqlc.arg(failure)::text,followup_attempts=followup_attempts+1,
 available_at=sqlc.arg(available_at)::timestamptz,lease_until=NULL,
 sent_at=CASE WHEN sqlc.arg(done)::boolean THEN clock_timestamp() END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint
AND delivery_state='sent' AND followup_pending;
-- name: EnqueueNotification :one
INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id,delivery_chat)
VALUES(sqlc.arg(booking_id)::text,sqlc.arg(owner)::text,sqlc.arg(kind)::text,
 CASE WHEN sqlc.arg(bot_id)::bigint>0 THEN sqlc.arg(bot_id)::bigint END,COALESCE((SELECT telegram_id FROM core.users WHERE id=sqlc.arg(owner)::text),0)) ON CONFLICT DO NOTHING RETURNING *;

-- name: LockNotificationRecipient :one
SELECT telegram_id,can_book FROM core.users WHERE id=sqlc.arg(owner)::text FOR SHARE;
-- name: QueueNotificationReminders :many
INSERT INTO core.massage_notices(booking_id,owner,kind,bot_id,delivery_chat)
	SELECT b.id,n.owner,n.kind,CASE WHEN sqlc.arg(bot_id)::bigint>0 THEN sqlc.arg(bot_id)::bigint END,COALESCE((SELECT telegram_id FROM core.users WHERE id=n.owner),0) FROM core.massage_bookings b
	JOIN core.massage_events e ON e.id=b.event_id
	JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
	CROSS JOIN LATERAL (VALUES
	 (b.owner,'prior_long',e.prior_long,true),
	 (b.owner,'prior_short',e.prior_short,true),
	 (b.specialist,'next',interval '5 minutes',sp.notify_next)) n(owner,kind,prior,enabled)
	WHERE b.event_id=sqlc.arg(event)::text AND b.cancelled_at IS NULL AND n.enabled
	AND b.starts_at >= (SELECT min(p.starts_at)-interval '2 hours' FROM core.massage_parties p WHERE p.event_id=b.event_id)
	AND b.starts_at<sqlc.arg(now)::timestamptz+n.prior
	ON CONFLICT DO NOTHING RETURNING *;

-- name: LockNotificationBooking :one
SELECT b.id FROM core.massage_bookings b JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
WHERE b.id=sqlc.arg(booking)::text FOR SHARE OF b,sp;

-- name: NotificationCurrent :one
SELECT (u.can_book AND (b.cancelled_at IS NULL OR n.kind='cancelled')
 AND (n.kind NOT IN ('booked','cancelled') OR sp.notify_bookings)
 AND (n.kind<>'next' OR sp.notify_next)
 AND (n.kind<>'additional' OR b.starts_at>=sqlc.arg(now)::timestamptz))::boolean AS current
FROM core.massage_notices n JOIN core.massage_bookings b ON b.id=n.booking_id
JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
JOIN core.users u ON u.id=n.owner WHERE n.id=sqlc.arg(id)::bigint AND n.bot_id=sqlc.arg(bot_id)::bigint;
-- name: NotificationReminderEvents :many
SELECT DISTINCT event_id FROM core.massage_parties
WHERE starts_at-interval '2 hours'<sqlc.arg(now)::timestamptz AND ends_at+interval '2 hours'>sqlc.arg(now)::timestamptz;

-- name: NotificationRecipients :many
SELECT n.owner,u.telegram_id FROM core.massage_notices n
JOIN core.users u ON u.id=n.owner
LEFT JOIN core.massage_notification_attempts a ON a.owner=n.owner
WHERE n.bot_id=sqlc.arg(bot_id)::bigint
AND (n.delivery_state='pending' OR (n.delivery_state='sent' AND n.followup_pending))
AND n.available_at<=clock_timestamp() AND (n.lease_until IS NULL OR n.lease_until<=clock_timestamp())
GROUP BY n.owner,u.telegram_id,a.attempted_at
ORDER BY a.attempted_at NULLS FIRST,n.owner LIMIT 100;

-- name: RecordNotificationRotation :exec
INSERT INTO core.massage_notification_attempts(owner,attempted_at)
SELECT id,clock_timestamp() FROM core.users WHERE id=sqlc.arg(owner)::text
ON CONFLICT(owner) DO UPDATE SET attempted_at=EXCLUDED.attempted_at;

-- name: NotificationBookingProjection :one
SELECT b.id,b.starts_at,b.ends_at,b.length,b.price,b.cancelled_at,u.name AS client,sp.name AS specialist
FROM core.massage_bookings b JOIN core.users u ON u.id=b.owner
JOIN core.massage_specialists sp ON sp.event_id=b.event_id AND sp.owner=b.specialist
WHERE b.id=sqlc.arg(booking)::text;


-- name: ExpiredNotification :one
SELECT * FROM core.massage_notices
WHERE bot_id=sqlc.arg(bot_id)::bigint
AND (delivery_state='unknown' OR (delivery_state='sending' AND lease_until<=clock_timestamp()))
ORDER BY id LIMIT 1;

-- name: PauseUnroutableNotification :exec
UPDATE core.massage_notices SET delivery_state='paused',failure='notification_identity_unavailable'
WHERE id=sqlc.arg(id)::bigint AND delivery_state='pending' AND (bot_id IS NULL OR delivery_chat=0);
-- name: MissingNotificationIndex :one
SELECT EXISTS(SELECT 1 FROM core.massage_notices n
WHERE n.bot_id=sqlc.arg(bot_id)::bigint AND n.delivery_chat<>0
AND (n.delivery_state IN ('pending','sending','unknown','parked','paused') OR n.followup_pending)
AND NOT EXISTS(SELECT 1 FROM core.delivery_queue q WHERE q.bot_id=n.bot_id
 AND q.owner_kind='massage' AND q.owner_key=n.id::text AND q.effect_key='send'))::boolean;
-- name: RescheduleUncertainNotification :one
WITH retry AS (
 SELECT id,clock_timestamp() AS observed_at,
  GREATEST(available_at,sqlc.arg(provider_deadline)::timestamptz,
   clock_timestamp()+sqlc.arg(fallback_seconds)::bigint *
    CASE uncertain_resends WHEN 0 THEN 1 WHEN 1 THEN 2 WHEN 2 THEN 4 ELSE 0 END * interval '1 second') AS next_at,
  uncertain_resends>=3 AND (sqlc.arg(uncertain)::boolean
   OR sqlc.arg(provider_deadline)::timestamptz<=clock_timestamp()) AS exhausted
 FROM core.massage_notices
 WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint
  AND delivery_attempt=sqlc.arg(attempt)::bigint AND delivery_state IN ('pending','sending','unknown')
  AND (sqlc.arg(uncertain)::boolean OR last_uncertain_attempt IS NOT NULL)
)
UPDATE core.massage_notices n SET
 last_uncertain_attempt=CASE WHEN sqlc.arg(uncertain)::boolean THEN n.delivery_attempt ELSE n.last_uncertain_attempt END,
 last_uncertain_reason=CASE WHEN sqlc.arg(uncertain)::boolean THEN 'telegram_outcome_unknown' ELSE n.last_uncertain_reason END,
 last_uncertain_recorded_at=CASE WHEN sqlc.arg(uncertain)::boolean
  AND n.last_uncertain_attempt IS DISTINCT FROM n.delivery_attempt THEN retry.observed_at ELSE n.last_uncertain_recorded_at END,
 delivery_state=CASE WHEN retry.exhausted THEN 'failed'
  WHEN retry.next_at>'9999-12-31T23:59:59.999999Z'::timestamptz THEN 'parked' ELSE 'pending' END,
 failure=CASE WHEN retry.exhausted THEN 'telegram_uncertain_retry_exhausted'
  WHEN retry.next_at>'9999-12-31T23:59:59.999999Z'::timestamptz THEN 'telegram_invalid_cooldown' ELSE n.failure END,
 available_at=CASE WHEN retry.exhausted OR retry.next_at>'9999-12-31T23:59:59.999999Z'::timestamptz
  THEN retry.observed_at ELSE retry.next_at END,
 lease_until=NULL,
 sent_at=CASE WHEN retry.exhausted THEN retry.observed_at ELSE n.sent_at END
FROM retry WHERE n.id=retry.id RETURNING n.delivery_state,n.available_at;
-- name: RecordNotificationUncertainty :execrows
UPDATE core.massage_notices SET
 last_uncertain_recorded_at=CASE WHEN last_uncertain_attempt IS DISTINCT FROM delivery_attempt
  THEN clock_timestamp() ELSE last_uncertain_recorded_at END,
 last_uncertain_attempt=delivery_attempt,last_uncertain_reason='telegram_outcome_unknown'
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint
 AND delivery_attempt=sqlc.arg(attempt)::bigint AND delivery_state IN ('sending','unknown');
-- name: RecordTerminalNotificationReceipt :execrows
UPDATE core.massage_notices SET telegram_message_id=sqlc.arg(message_id)::bigint
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint
 AND delivery_attempt=sqlc.arg(attempt)::bigint AND last_uncertain_attempt=delivery_attempt
 AND delivery_state IN ('failed','cancelled') AND lease_until IS NULL
 AND last_confirmed_attempt IS DISTINCT FROM delivery_attempt
 AND sqlc.arg(message_id)::bigint>0 AND telegram_message_id IN (0,sqlc.arg(message_id)::bigint);

-- name: RecordTerminalNotificationConfirmation :execrows
UPDATE core.massage_notices SET last_confirmed_attempt=delivery_attempt
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint
 AND delivery_attempt=sqlc.arg(attempt)::bigint AND last_uncertain_attempt=delivery_attempt
 AND delivery_state IN ('failed','cancelled') AND lease_until IS NULL AND telegram_message_id=0;
