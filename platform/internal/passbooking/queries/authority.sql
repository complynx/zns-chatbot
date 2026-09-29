-- name: LockReadEvents :many
SELECT id FROM core.pass_events WHERE id=ANY($1::text[]) ORDER BY id FOR SHARE;

-- name: LockReadActor :one
SELECT telegram_id FROM core.users WHERE id=$1 FOR SHARE;

-- name: LockReadBooking :one
SELECT owner,version,created_at,state,invitation_target,COALESCE(payment_attempt,'') AS payment_attempt
FROM core.pass_bookings WHERE event_id=$1 AND owner=$2 FOR SHARE;

-- name: LockReadTarget :one
SELECT id,can_book FROM core.users WHERE telegram_id=$1 FOR SHARE;

-- name: LockReadPaymentAttempt :one
SELECT decision='pending' AS pending FROM core.pass_payment_attempts WHERE event_id=$1 AND id=$2 FOR SHARE;
