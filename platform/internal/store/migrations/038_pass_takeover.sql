-- Legacy receiving provenance is distinct from an immutable receipt attempt.
CREATE TABLE core.pass_receiver_backfills (
 event_id text NOT NULL, owner text NOT NULL, assigned_at timestamptz NOT NULL,
 legacy_receiving_admin text NOT NULL REFERENCES core.users(id),
 recorded_at timestamptz NOT NULL,
 PRIMARY KEY(event_id,owner,assigned_at),
 FOREIGN KEY(event_id,owner) REFERENCES core.pass_bookings(event_id,owner)
);
