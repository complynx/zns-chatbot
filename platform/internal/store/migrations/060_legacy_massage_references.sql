-- Exact source evidence and stable identifiers survive the disposable importer.
CREATE TABLE core.legacy_massage_import_references (
 source_key text PRIMARY KEY CHECK(source_key ~ '^[0-9a-f]{64}$'),
 bot_id bigint NOT NULL CHECK(bot_id>0),
 source_kind text NOT NULL CHECK(source_kind IN ('booking','draft','specialist','configuration','excluded')),
 source_record_sha256 text NOT NULL CHECK(source_record_sha256 ~ '^[0-9a-f]{64}$'),
 source_record jsonb NOT NULL,
 target_id text NOT NULL,
 owner text REFERENCES core.users(id),
 event_id text REFERENCES core.massage_events(id)
);
ALTER TABLE core.legacy_user_deferred_domains DROP CONSTRAINT legacy_user_deferred_domains_domain_check;
ALTER TABLE core.legacy_user_deferred_domains ADD CONSTRAINT legacy_user_deferred_domains_domain_check CHECK(domain IN ('passes','food','massage'));
CREATE TABLE core.legacy_massage_drafts (
 id text PRIMARY KEY,
 source_key text UNIQUE NOT NULL REFERENCES core.legacy_massage_import_references(source_key),
 owner text NOT NULL REFERENCES core.users(id),
 event_id text NOT NULL REFERENCES core.massage_events(id),
 version bigint NOT NULL DEFAULT 0 CHECK(version>=0),
 state jsonb NOT NULL CHECK(jsonb_typeof(state)='object'),
 booking_id text REFERENCES core.massage_bookings(id),
 closed boolean NOT NULL DEFAULT false
);
