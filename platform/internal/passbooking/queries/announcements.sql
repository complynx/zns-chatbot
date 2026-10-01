-- name: EnqueueRegistrationAnnouncements :exec
INSERT INTO core.pass_registration_announcements(event_id,owner,created_at,channel,thread_id,locale,name,role,state,bot_id)
SELECT b.event_id,b.owner,b.created_at,e.thread_channel,e.thread_id,e.thread_locale,u.name,b.role,
 CASE WHEN e.thread_channel='' THEN 'suppressed' ELSE 'pending' END,CASE WHEN sqlc.arg(bot_id)::bigint>0 THEN sqlc.arg(bot_id)::bigint END
FROM core.pass_bookings b JOIN core.pass_events e ON e.id=b.event_id JOIN core.users u ON u.id=b.owner
WHERE b.event_id=sqlc.arg(event_id)::text AND (e.open_ended OR e.finishes_at>COALESCE(sqlc.narg(domain_time)::timestamptz,clock_timestamp()))
ON CONFLICT(event_id,owner,created_at) DO NOTHING;

-- name: ExpireAnnouncementSends :exec
UPDATE core.pass_registration_announcements SET state='unknown',failure='telegram_outcome_unknown'
WHERE bot_id=sqlc.arg(bot_id)::bigint AND state='sending' AND lease_until<=clock_timestamp();

-- name: PrepareAnnouncement :one
WITH next AS (
 SELECT a.id FROM core.pass_registration_announcements a
 WHERE a.bot_id=sqlc.arg(bot_id)::bigint AND a.state='pending' AND a.available_at<=clock_timestamp()
 AND (a.lease_until IS NULL OR a.lease_until<=clock_timestamp())
 AND NOT EXISTS(SELECT 1 FROM core.pass_registration_announcements earlier
  WHERE (earlier.bot_id=a.bot_id OR earlier.bot_id IS NULL) AND earlier.channel=a.channel
   AND COALESCE(earlier.thread_id,0)=COALESCE(a.thread_id,0) AND earlier.id<a.id
   AND earlier.state IN ('pending','sending','unknown','parked','paused'))
 ORDER BY a.id FOR UPDATE SKIP LOCKED LIMIT 1
)
UPDATE core.pass_registration_announcements a
SET attempts=attempts+1,lease_until=clock_timestamp()+interval '2 minutes'
FROM next WHERE a.id=next.id RETURNING a.id,a.channel,a.thread_id,a.locale,a.name,a.role,a.attempts;

-- name: LockAnnouncementAttempt :one
SELECT a.channel,a.thread_id,a.state,a.message_id,a.available_at,a.failure,
 a.last_uncertain_attempt,a.uncertain_resends,a.lease_until,
 COALESCE(a.lease_until>clock_timestamp(),false)::boolean AS lease_live,
 EXISTS(SELECT 1 FROM core.pass_events e JOIN core.pass_bookings b ON b.event_id=e.id
  WHERE e.id=a.event_id AND b.owner=a.owner AND b.created_at=a.created_at AND b.state<>'cancelled'
   AND (e.open_ended OR e.finishes_at>COALESCE(sqlc.narg(domain_time)::timestamptz,clock_timestamp()))
   AND e.thread_channel=a.channel AND COALESCE(e.thread_id,0)=COALESCE(a.thread_id,0))::boolean AS current
FROM core.pass_registration_announcements a WHERE a.id=sqlc.arg(id)::bigint
 AND a.bot_id=sqlc.arg(bot_id)::bigint AND a.attempts=sqlc.arg(attempt)::bigint FOR UPDATE;

-- name: BeginAnnouncementSend :execrows
UPDATE core.pass_registration_announcements SET state='sending',lease_until=clock_timestamp()+interval '2 minutes',
 uncertain_resends=uncertain_resends+CASE WHEN last_uncertain_attempt IS NOT NULL THEN 1 ELSE 0 END
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempts=sqlc.arg(attempt)::bigint
 AND state='pending' AND lease_until>clock_timestamp()
 AND (last_uncertain_attempt IS NULL OR uncertain_resends<3);

-- name: RecordAnnouncementUncertainty :exec
UPDATE core.pass_registration_announcements SET last_uncertain_attempt=sqlc.arg(attempt)::bigint,
 last_uncertain_reason=sqlc.arg(reason)::text,last_uncertain_recorded_at=clock_timestamp()
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempts=sqlc.arg(attempt)::bigint
 AND (last_uncertain_attempt IS NULL OR last_uncertain_attempt<sqlc.arg(attempt)::bigint);

-- name: FinishAnnouncement :execrows
UPDATE core.pass_registration_announcements SET state=sqlc.arg(state)::text,
 message_id=sqlc.arg(message_id)::bigint,failure=sqlc.arg(failure)::text,
 available_at=sqlc.arg(available_at)::timestamptz,lease_until=NULL,
 failure_count=failure_count+sqlc.arg(failure_increment)::bigint
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempts=sqlc.arg(attempt)::bigint
 AND state IN ('pending','sending','unknown');

-- name: RecordAnnouncementTerminalReceipt :execrows
UPDATE core.pass_registration_announcements SET message_id=sqlc.arg(message_id)::bigint
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint
 AND attempts=sqlc.arg(attempt)::bigint AND last_uncertain_attempt=sqlc.arg(attempt)::bigint
 AND state IN ('failed','cancelled') AND lease_until IS NULL
 AND sqlc.arg(message_id)::bigint>0
 AND (message_id=0 OR message_id=sqlc.arg(message_id)::bigint);

-- name: AnnouncementAttemptSource :one
SELECT event_id,owner FROM core.pass_registration_announcements
WHERE id=sqlc.arg(id)::bigint AND bot_id=sqlc.arg(bot_id)::bigint AND attempts=sqlc.arg(attempt)::bigint;

-- name: LockAnnouncementEvent :one
SELECT id FROM core.pass_events WHERE id=sqlc.arg(event_id)::text FOR SHARE;

-- name: LockAnnouncementBooking :one
SELECT owner FROM core.pass_bookings
WHERE event_id=sqlc.arg(event_id)::text AND owner=sqlc.arg(owner)::text FOR SHARE;

