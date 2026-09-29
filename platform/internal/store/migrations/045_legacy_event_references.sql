-- Durable source attribution remains after temporary import tooling is removed.
CREATE TABLE core.legacy_event_references (
 source_key text PRIMARY KEY CHECK (source_key ~ '^[0-9a-f]{64}$'),
 event_id text UNIQUE NOT NULL REFERENCES core.pass_events(id),
 source_record_sha256 text NOT NULL CHECK (source_record_sha256 ~ '^[0-9a-f]{64}$')
);
