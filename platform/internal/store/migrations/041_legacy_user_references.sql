-- Durable references survive removal of the one-time import tooling.
CREATE TABLE core.legacy_user_references (
 source_key text PRIMARY KEY CHECK (source_key ~ '^[0-9a-f]{64}$'),
 owner text UNIQUE NOT NULL REFERENCES core.users(id),
 source_record_sha256 text NOT NULL CHECK (source_record_sha256 ~ '^[0-9a-f]{64}$')
);
