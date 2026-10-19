-- +goose Up
CREATE TABLE shuttle_events (
 id uuid PRIMARY KEY, bot_id bigint NOT NULL, organization_id text NOT NULL,
 name text NOT NULL CHECK(char_length(name) BETWEEN 1 AND 256), timezone text NOT NULL,
 starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL CHECK(ends_at>starts_at),
 modules text[] NOT NULL DEFAULT '{}' CHECK(cardinality(modules)<=100),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0)
);
CREATE INDEX shuttle_events_scope ON shuttle_events(bot_id,organization_id,id);
CREATE TABLE shuttle_event_operations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, request_hash bytea NOT NULL, result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Administrative event configuration and operation receipts must be exported before rollback.
DROP TABLE shuttle_event_operations;
DROP TABLE shuttle_events;
