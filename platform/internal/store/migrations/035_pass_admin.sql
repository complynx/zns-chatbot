ALTER TABLE core.pass_bookings DROP CONSTRAINT pass_bookings_kind_check;
ALTER TABLE core.pass_bookings DROP CONSTRAINT pass_bookings_check2;
ALTER TABLE core.pass_bookings ADD CONSTRAINT pass_kind_length CHECK(char_length(kind) BETWEEN 1 AND 80);
ALTER TABLE core.pass_bookings ADD CONSTRAINT pass_partner_state CHECK(partner='' OR state IN ('waitlist','assigned','paid'));
ALTER TABLE core.pass_bookings ADD COLUMN comment text NOT NULL DEFAULT '' CHECK(char_length(comment)<=2000);

ALTER TABLE core.pass_payment_attempts ADD COLUMN kind text NOT NULL DEFAULT 'receipt' CHECK(kind IN ('receipt','free'));
ALTER TABLE core.pass_payment_attempts ALTER COLUMN proof_id DROP NOT NULL;
ALTER TABLE core.pass_payment_attempts ADD CONSTRAINT pass_payment_proof_kind CHECK((kind='free')=(proof_id IS NULL));
ALTER TABLE core.pass_payment_attempts ADD CONSTRAINT pass_free_accepted CHECK(kind<>'free' OR decision='accepted');

-- Append-only service ledger; snapshots contain booking fields, never passport/profile values.
CREATE TABLE core.pass_admin_assignments (
 event_id text NOT NULL REFERENCES core.pass_events(id), actor text NOT NULL REFERENCES core.users(id),
 key_hash text NOT NULL, target text NOT NULL REFERENCES core.users(id),
 assigned_count integer NOT NULL CHECK(assigned_count BETWEEN 1 AND 2),
 before_records jsonb NOT NULL, after_records jsonb NOT NULL,
 at timestamptz NOT NULL,
 PRIMARY KEY(event_id,actor,key_hash),
 FOREIGN KEY(event_id,actor,key_hash) REFERENCES core.pass_booking_operations(event_id,actor,key_hash)
);
