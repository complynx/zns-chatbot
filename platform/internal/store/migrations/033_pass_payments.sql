CREATE TABLE core.pass_payment_attempts (
 id text PRIMARY KEY, event_id text NOT NULL REFERENCES core.pass_events(id),
 submitter text NOT NULL REFERENCES core.users(id), proof_id text NOT NULL REFERENCES core.order_proofs(id),
 receiving_admin text NOT NULL REFERENCES core.users(id), received_at timestamptz NOT NULL,
 decision text NOT NULL DEFAULT 'pending' CHECK(decision IN ('pending','accepted','rejected')),
 reviewed_by text REFERENCES core.users(id), reviewed_at timestamptz,
 CHECK((decision='pending' AND reviewed_at IS NULL AND reviewed_by IS NULL)
 OR (decision<>'pending' AND reviewed_at IS NOT NULL AND reviewed_by IS NOT NULL)),
 UNIQUE(event_id,id)
);
CREATE TABLE core.pass_payment_participants (
 attempt text NOT NULL REFERENCES core.pass_payment_attempts(id),
 owner text NOT NULL REFERENCES core.users(id), assigned_at timestamptz NOT NULL,
 PRIMARY KEY(attempt,owner)
);
ALTER TABLE core.pass_bookings ADD COLUMN payment_attempt text;
ALTER TABLE core.pass_bookings ADD CONSTRAINT pass_payment_event_fk
 FOREIGN KEY(event_id,payment_attempt) REFERENCES core.pass_payment_attempts(event_id,id);
CREATE INDEX pass_payment_pending ON core.pass_payment_attempts(event_id,received_at,id) WHERE decision='pending';
