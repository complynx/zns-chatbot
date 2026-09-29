-- name: HasUnsubmittedPayment :one
SELECT EXISTS (
  SELECT 1 FROM core.pass_bookings booking
  WHERE booking.event_id=sqlc.arg(event)::text AND booking.owner=sqlc.arg(owner)::text
    AND booking.state='assigned' AND booking.payment_attempt IS NULL
    AND NOT EXISTS (
      SELECT 1 FROM core.legacy_pass_payment_metadata metadata
      WHERE metadata.event_id=booking.event_id AND metadata.owner=booking.owner
    )
) AS absent;
