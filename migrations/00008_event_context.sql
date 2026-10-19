-- +goose Up
CREATE TABLE shuttle_user_event_context (
 user_id uuid PRIMARY KEY REFERENCES shuttle_users(id), event_id uuid REFERENCES shuttle_events(id),
 version bigint NOT NULL DEFAULT 0 CHECK(version>=0)
);
CREATE TABLE shuttle_run_event_context (
 user_id uuid NOT NULL REFERENCES shuttle_users(id), run_id text NOT NULL CHECK(char_length(run_id) BETWEEN 1 AND 128),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 PRIMARY KEY(user_id,run_id)
);
CREATE TABLE shuttle_event_context_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id), operation_id text NOT NULL,
 request_hash bytea NOT NULL, result jsonb NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(user_id,operation_id)
);
-- +goose Down
-- Export selections and receipts before intentionally rolling back data. Binary rollback keeps them.
DROP TABLE shuttle_event_context_operations;
DROP TABLE shuttle_run_event_context;
DROP TABLE shuttle_user_event_context;
