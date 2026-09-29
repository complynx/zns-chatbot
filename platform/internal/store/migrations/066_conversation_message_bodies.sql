-- Long history retains one event and never becomes a memory document.
ALTER TABLE core.conversation_events ADD COLUMN omission_reason text NOT NULL DEFAULT '';
CREATE TABLE core.conversation_message_bodies (
 event_id bigint PRIMARY KEY REFERENCES core.conversation_events(id) ON DELETE CASCADE,
 body text NOT NULL CHECK(octet_length(body)<=16777216),
 body_sha256 text NOT NULL CHECK(body_sha256=encode(sha256(convert_to(body,'UTF8')),'hex')),
 character_count integer NOT NULL CHECK(character_count=char_length(body))
);
CREATE TABLE core.conversation_history_generations (
 owner text PRIMARY KEY REFERENCES core.users(id),
 generation bigint NOT NULL DEFAULT 0 CHECK(generation>=0)
);
-- Hashes bind private source archives without copying their metadata or bodies.
CREATE TABLE core.legacy_message_references (
 source_key text PRIMARY KEY CHECK(source_key ~ '^[0-9a-f]{64}$'),
 event_id bigint UNIQUE REFERENCES core.conversation_events(id),
 owner text REFERENCES core.users(id),
 bot_namespace text NOT NULL,
 collection text NOT NULL,
 identity_sha256 text NOT NULL CHECK(identity_sha256 ~ '^[0-9a-f]{64}$'),
 source_record_sha256 text NOT NULL CHECK(source_record_sha256 ~ '^[0-9a-f]{64}$'),
 resolved_at timestamptz NOT NULL,
 plan_sha256 text NOT NULL CHECK(plan_sha256 ~ '^[0-9a-f]{64}$'),
 resolution_sha256 text NOT NULL CHECK(resolution_sha256 ~ '^[0-9a-f]{64}$'),
 disposition text NOT NULL CHECK(disposition IN ('retained','omitted','excluded')),
 tombstoned boolean NOT NULL DEFAULT false
);
