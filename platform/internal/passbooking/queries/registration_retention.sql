-- name: InitializeRegistrationTurns :exec
UPDATE core.registration_intents
SET effective_position=COALESCE(effective_position,ingress_id),
 turn_expires_at=checked_at+(sqlc.arg(retention_microseconds)::bigint*interval '1 microsecond')
WHERE event_id=sqlc.arg(event_id) AND origin='canonical_ingress'
 AND state='captured' AND turn_expires_at IS NULL;

-- name: ExpiredRegistrationTurns :many
SELECT id FROM core.registration_intents
WHERE event_id=$1 AND state='captured' AND turn_expires_at<=sqlc.arg(observed_at)::timestamptz
ORDER BY effective_position,id;

-- name: RotateRegistrationTurn :exec
UPDATE core.registration_intents
SET effective_position=nextval('core.registration_ingress_id_seq'),
 turn_expires_at=sqlc.arg(deadline)::timestamptz,requeue_count=requeue_count+1
WHERE id=sqlc.arg(id) AND state='captured' AND turn_expires_at<=sqlc.arg(observed_at)::timestamptz;

-- name: RegistrationRanks :many
SELECT owner,COALESCE(effective_position,0)::bigint AS position,state
FROM core.registration_intents WHERE event_id=$1 AND origin='canonical_ingress' AND state<>'cancelled';

-- name: PendingNativeRegistrationRanks :many
SELECT g.native_owner::text AS owner,MIN(COALESCE(i.effective_position,g.id))::bigint AS position
FROM core.registration_ingress g
LEFT JOIN core.registration_intents i ON i.event_id=g.native_event AND i.owner=g.native_owner AND i.state<>'cancelled'
WHERE g.native_event=sqlc.arg(event)::text AND g.native_payload IS NOT NULL AND g.native_outcome=''
 AND (i.id IS NULL OR i.state='captured')
 AND NOT EXISTS(SELECT 1 FROM core.registration_intents retired
  WHERE retired.event_id=g.native_event AND retired.owner=g.native_owner
  AND retired.state='cancelled' AND retired.closed_through_position>=g.id)
GROUP BY g.native_owner;

-- name: NativeAdmissionEvidence :one
SELECT received_at,native_outcome FROM core.registration_ingress
WHERE id=sqlc.arg(id)::bigint AND native_event=sqlc.arg(event)::text AND native_owner=sqlc.arg(owner)::text
 AND native_payload->'command'->>'key'=sqlc.arg(key)::text;
