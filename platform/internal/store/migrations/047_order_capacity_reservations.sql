CREATE TABLE core.order_capacity_slots (
  event_id text NOT NULL REFERENCES core.order_events(id),
  service text NOT NULL,
  seat integer NOT NULL CHECK (seat >= 0),
  reservation_id text,
  reservation_attempt_token text,
  reservation_attempt_created_at timestamptz,
  reserved_at timestamptz,
  PRIMARY KEY (event_id, service, seat),
  CHECK (reservation_id IS NOT NULL OR
    (reservation_attempt_token IS NULL AND reservation_attempt_created_at IS NULL AND reserved_at IS NULL))
);
CREATE UNIQUE INDEX order_capacity_reservation
  ON core.order_capacity_slots(event_id, service, reservation_id)
  WHERE reservation_id IS NOT NULL;

-- Existing paid/proof choices already own capacity. Fail rather than discard
-- allocations if an inconsistent old catalog cannot represent all of them.
DO $$
BEGIN
  IF EXISTS (
    SELECT 1 FROM core.order_events e CROSS JOIN LATERAL jsonb_each(e.extras) x
    WHERE (x.value->>'capacity')::integer > 0 AND
      (SELECT count(*) FROM core.orders o WHERE o.event_id=e.id
       AND o.state IN ('proof','paid') AND o.choice->'extras' ? x.key) > (x.value->>'capacity')::integer
  ) THEN
    RAISE EXCEPTION 'order_capacity_existing_allocations_exceed_limit';
  END IF;
END $$;

INSERT INTO core.order_capacity_slots(event_id,service,seat)
SELECT e.id,x.key,seat FROM core.order_events e CROSS JOIN LATERAL jsonb_each(e.extras) x
CROSS JOIN LATERAL generate_series(0,(x.value->>'capacity')::integer-1) seat
WHERE (x.value->>'capacity')::integer > 0;

WITH allocations AS (
  SELECT o.id,o.event_id,x.key AS service,o.attempt,o.proof_file,
    COALESCE(o.attempt_at,o.created_at) AS attempt_at,
    row_number() OVER (PARTITION BY o.event_id,x.key ORDER BY o.attempt_at NULLS LAST,o.created_at,o.id)-1 AS seat
  FROM core.orders o JOIN core.order_events e ON e.id=o.event_id
  CROSS JOIN LATERAL jsonb_each(e.extras) x
  WHERE o.state IN ('proof','paid') AND (x.value->>'capacity')::integer > 0 AND o.choice->'extras' ? x.key
)
UPDATE core.order_capacity_slots s SET reservation_id=a.id,
  reservation_attempt_token=CASE WHEN a.attempt<>'' THEN a.attempt
    WHEN a.proof_file<>'' THEN 'legacy-proof:'||a.proof_file ELSE 'legacy-order:'||a.id END,
  reservation_attempt_created_at=a.attempt_at,reserved_at=a.attempt_at
FROM allocations a WHERE s.event_id=a.event_id AND s.service=a.service AND s.seat=a.seat;
