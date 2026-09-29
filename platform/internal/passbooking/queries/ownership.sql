-- name: OwnsEventBookings :one
SELECT NOT EXISTS (
  SELECT 1 FROM unnest(sqlc.arg(events)::text[]) AS requested(event_id)
  WHERE NOT EXISTS (
    SELECT 1 FROM core.pass_bookings booking
    WHERE booking.event_id=requested.event_id AND booking.owner=sqlc.arg(owner)::text
  )
) AS all_owned;
