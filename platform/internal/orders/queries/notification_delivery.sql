-- name: PrepareNotification :one
WITH head AS (
 SELECT n.id,COALESCE(u.telegram_id,0)::bigint AS chat FROM core.order_notifications n JOIN core.users u ON u.id=n.recipient
 WHERE n.bot_id=sqlc.arg(bot_id)::bigint
 AND (sqlc.arg(id)::bigint=0 OR n.id=sqlc.arg(id)::bigint)
 AND (NOT sqlc.arg(followup_only)::boolean OR (n.delivery_state='sent' AND n.followup_pending))
 AND (sqlc.arg(owner)::text='' OR n.recipient=sqlc.arg(owner)::text)
 AND (n.delivery_state='pending' OR (n.delivery_state='sent' AND n.followup_pending))
 AND n.available_at<=clock_timestamp() AND (n.lease_until IS NULL OR n.lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM core.order_notifications older
  JOIN core.delivery_queue oq ON oq.bot_id=older.bot_id AND oq.owner_kind='orders'
   AND oq.owner_key=older.id::text AND oq.effect_key='send'
  JOIN core.delivery_queue nq ON nq.bot_id=n.bot_id AND nq.owner_kind='orders'
   AND nq.owner_key=n.id::text AND nq.effect_key='send'
  WHERE older.bot_id=n.bot_id AND older.delivery_chat=n.delivery_chat
   AND older.followup_pending AND oq.lane_sequence<nq.lane_sequence)
 AND (n.delivery_state='sent' OR EXISTS(SELECT 1 FROM core.delivery_queue q
  WHERE q.bot_id=n.bot_id AND q.owner_kind='orders' AND q.owner_key=n.id::text AND q.effect_key='send'
  AND NOT EXISTS(SELECT 1 FROM core.delivery_queue previous WHERE previous.bot_id=q.bot_id
   AND previous.chat=q.chat AND previous.lane_sequence<q.lane_sequence
   AND previous.state IN ('pending','sending','unknown','parked','paused'))))
 ORDER BY n.available_at,n.id LIMIT 1 FOR UPDATE OF n SKIP LOCKED
)
UPDATE core.order_notifications n SET delivery_attempt=delivery_attempt+1,lease_until=clock_timestamp()+interval '2 minutes'
FROM head WHERE n.id=head.id RETURNING n.*;

-- name: ReadNotification :one
SELECT * FROM core.order_notifications WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint;

-- name: LockNotificationAttempt :one
SELECT *,COALESCE(lease_until>clock_timestamp(),false)::boolean AS lease_live FROM core.order_notifications
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint FOR UPDATE;

-- name: BeginNotificationSend :execrows
UPDATE core.order_notifications SET delivery_state='sending',lease_until=clock_timestamp()+interval '2 minutes'
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint
AND delivery_state='pending' AND lease_until>clock_timestamp();

-- name: FinishNotificationDelivery :execrows
UPDATE core.order_notifications SET delivery_state=sqlc.arg(state)::text,
 telegram_message_id=sqlc.arg(message_id)::bigint,delivery_text=sqlc.arg(text)::text,failure=sqlc.arg(failure)::text,
 available_at=sqlc.arg(available_at)::timestamptz,
 lease_until=CASE WHEN sqlc.arg(state)::text='sent' THEN clock_timestamp()+interval '2 minutes' END,
 followup_pending=(sqlc.arg(state)::text='sent'),
 failure_count=failure_count+sqlc.arg(failure_increment)::bigint,
 delivered_at=CASE WHEN sqlc.arg(state)::text IN ('failed','cancelled') THEN clock_timestamp() END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint
AND delivery_state IN ('pending','sending','unknown');

-- name: FinishNotificationFollowup :execrows
UPDATE core.order_notifications SET followup_pending=NOT sqlc.arg(done)::boolean,
 followup_failure=sqlc.arg(failure)::text,followup_attempts=followup_attempts+1,
 available_at=sqlc.arg(available_at)::timestamptz,lease_until=NULL,
 delivered_at=CASE WHEN sqlc.arg(done)::boolean THEN clock_timestamp() END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND delivery_attempt=sqlc.arg(attempt)::bigint
AND delivery_state='sent' AND followup_pending;
-- name: EnqueueNotification :one
INSERT INTO core.order_notifications(recipient,order_id,payload,bot_id,delivery_chat)
VALUES(sqlc.arg(recipient)::text,sqlc.arg(order_id)::text,sqlc.arg(payload)::jsonb,
 CASE WHEN sqlc.arg(bot_id)::bigint>0 THEN sqlc.arg(bot_id)::bigint END,COALESCE((SELECT telegram_id FROM core.users WHERE id=sqlc.arg(recipient)::text),0)) RETURNING *;

-- name: LockNotificationRecipient :one
SELECT telegram_id,can_book FROM core.users WHERE id=sqlc.arg(owner)::text FOR SHARE;
-- name: NotificationEvent :one
SELECT event_id FROM core.orders WHERE id=sqlc.arg(order_id)::text;

-- name: LockNotificationOrder :one
SELECT id FROM core.orders WHERE id=sqlc.arg(order_id)::text FOR UPDATE;

-- name: NotificationCurrent :one
SELECT (u.can_book AND CASE WHEN n.payload->>'kind'='refund_request' THEN
 EXISTS(SELECT 1 FROM core.order_refund_tasks r
 JOIN core.pass_bookings b ON b.event_id=r.event_id AND b.owner=r.owner
 WHERE r.id=(n.payload->>'refund_id')::bigint AND r.version=(n.payload->>'refund_version')::bigint
 AND r.state='pending' AND r.order_id=n.order_id AND r.notification_id=n.id
 AND r.ambassador=n.recipient AND b.payment_admin=n.recipient AND b.state<>'cancelled'
 AND (EXISTS(SELECT 1 FROM core.pass_booking_admins a WHERE a.owner=n.recipient)
 OR EXISTS(SELECT 1 FROM core.pass_payment_admins a WHERE a.event_id=r.event_id AND a.owner=n.recipient)))
 WHEN n.payload->>'kind'='payment_request' THEN
 o.state IN ('proof','cash') AND o.attempt=n.payload->>'attempt' AND o.payment_admin=n.recipient
 AND EXISTS(SELECT 1 FROM core.order_admins a WHERE a.event_id=o.event_id AND a.owner=n.recipient)
 WHEN n.payload->>'kind' IN ('accept','reject') THEN o.state=n.payload->>'state'
 WHEN n.payload->>'kind'='reminder' THEN o.state IN ('unpaid','cash') AND (o.choice->>'total')::numeric>0
 ELSE true END)::boolean AS current
FROM core.order_notifications n JOIN core.users u ON u.id=n.recipient JOIN core.orders o ON o.id=n.order_id
WHERE n.id=sqlc.arg(id)::bigint AND n.bot_id=sqlc.arg(bot_id)::bigint;


-- name: ExpiredNotification :one
SELECT * FROM core.order_notifications
WHERE bot_id=sqlc.arg(bot_id)::bigint AND delivery_state='sending' AND lease_until<=clock_timestamp()
ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED;

-- name: PauseUnroutableNotification :exec
UPDATE core.order_notifications SET delivery_state='paused',failure='notification_identity_unavailable'
WHERE id=sqlc.arg(id)::bigint AND delivery_state='pending' AND (bot_id IS NULL OR delivery_chat=0);
-- name: MissingNotificationIndex :one
SELECT EXISTS(SELECT 1 FROM core.order_notifications n
WHERE n.bot_id=sqlc.arg(bot_id)::bigint AND n.delivery_chat<>0
AND (n.delivery_state IN ('pending','sending','unknown','parked','paused') OR n.followup_pending)
AND NOT EXISTS(SELECT 1 FROM core.delivery_queue q WHERE q.bot_id=n.bot_id
 AND q.owner_kind='orders' AND q.owner_key=n.id::text AND q.effect_key='send'))::boolean;
