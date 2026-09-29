-- Imported source facts survive retirement of the offline migration tool.
CREATE TABLE core.legacy_pass_import_references (
 source_key text PRIMARY KEY CHECK(source_key ~ '^[0-9a-f]{64}$'),
 bot_id bigint NOT NULL CHECK(bot_id>0),
 event_id text REFERENCES core.pass_events(id),
 owner text REFERENCES core.users(id),
 source_kind text NOT NULL CHECK(source_kind IN ('booking','embedded','preference','user_marker','proof')),
 source_record_sha256 text NOT NULL CHECK(source_record_sha256 ~ '^[0-9a-f]{64}$'),
 target_id text NOT NULL, source_record jsonb NOT NULL
);
CREATE TABLE core.legacy_pass_payment_metadata (
 event_id text NOT NULL, owner text NOT NULL, assigned_at timestamptz NOT NULL,
 source_key text NOT NULL REFERENCES core.legacy_pass_import_references(source_key),
 received_at timestamptz, accepted_at timestamptz, rejected_at timestamptz,
 receiving_admin text REFERENCES core.users(id), reviewed_by text REFERENCES core.users(id),
 proof_reference text NOT NULL DEFAULT '',
 PRIMARY KEY(event_id,owner,assigned_at),
 FOREIGN KEY(event_id,owner) REFERENCES core.pass_bookings(event_id,owner)
);
ALTER TABLE core.pass_payment_attempts ADD COLUMN legacy_source_key text
 REFERENCES core.legacy_pass_import_references(source_key);
ALTER TABLE core.pass_payment_attempts ADD COLUMN proof_unavailable boolean NOT NULL DEFAULT false;
ALTER TABLE core.pass_payment_attempts ALTER COLUMN submitter DROP NOT NULL;
ALTER TABLE core.pass_payment_attempts ADD CONSTRAINT pass_submitter_known
 CHECK(submitter IS NOT NULL OR legacy_source_key IS NOT NULL);
ALTER TABLE core.pass_payment_attempts DROP CONSTRAINT pass_payment_attempts_check;
ALTER TABLE core.pass_payment_attempts ADD CONSTRAINT pass_review_provenance CHECK(
 (decision='pending' AND reviewed_at IS NULL AND reviewed_by IS NULL) OR
 (decision<>'pending' AND reviewed_at IS NOT NULL AND (reviewed_by IS NOT NULL OR legacy_source_key IS NOT NULL)));
ALTER TABLE core.pass_payment_attempts DROP CONSTRAINT pass_payment_proof_kind;
ALTER TABLE core.pass_payment_attempts ADD CONSTRAINT pass_payment_proof_kind CHECK(
 (kind='free' AND proof_id IS NULL AND NOT proof_unavailable) OR
 (kind='receipt' AND ((proof_id IS NOT NULL AND NOT proof_unavailable) OR
 (proof_id IS NULL AND proof_unavailable AND legacy_source_key IS NOT NULL))));
CREATE TABLE core.pass_contact_preferences (
 event_id text NOT NULL REFERENCES core.pass_events(id), owner text NOT NULL REFERENCES core.users(id),
 payment_admin text NOT NULL REFERENCES core.users(id),
 PRIMARY KEY(event_id,owner)
);
CREATE TABLE core.pass_passport_reminders (
 owner text PRIMARY KEY REFERENCES core.users(id),
 legacy_source_key text REFERENCES core.legacy_pass_import_references(source_key),
 notified_at timestamptz,
 CHECK(legacy_source_key IS NOT NULL OR notified_at IS NOT NULL)
);
CREATE TABLE core.legacy_user_deferred_domains (
 source_key text NOT NULL REFERENCES core.legacy_user_references(source_key),
 domain text NOT NULL CHECK(domain IN ('passes','food')),
 source_record jsonb NOT NULL,
 completed boolean NOT NULL DEFAULT false,
 PRIMARY KEY(source_key,domain)
);

-- Imported shared receipts may have different known actors for each participant.
ALTER TABLE core.pass_payment_attempts ALTER COLUMN receiving_admin DROP NOT NULL;
ALTER TABLE core.pass_payment_attempts ADD CONSTRAINT pass_receiver_known
 CHECK(receiving_admin IS NOT NULL OR legacy_source_key IS NOT NULL);
-- Legacy reminder fields are independent; absence of the first is not a timestamp.
ALTER TABLE core.pass_deadline_markers ALTER COLUMN first_at DROP NOT NULL;

CREATE TABLE core.legacy_pass_assignment_metadata (
 event_id text NOT NULL, owner text NOT NULL, assigned_at timestamptz NOT NULL,
 source_key text NOT NULL REFERENCES core.legacy_pass_import_references(source_key),
 assignment_tier_number integer CHECK(assignment_tier_number>0),
 PRIMARY KEY(event_id,owner,assigned_at),
 FOREIGN KEY(event_id,owner) REFERENCES core.pass_bookings(event_id,owner)
);
CREATE TABLE core.legacy_pass_deferred_domains (
 source_key text NOT NULL REFERENCES core.legacy_pass_import_references(source_key),
 domain text NOT NULL CHECK(domain='food'), source_record jsonb NOT NULL,
 completed boolean NOT NULL DEFAULT false, PRIMARY KEY(source_key,domain)
);
