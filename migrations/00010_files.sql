-- +goose Up
CREATE TABLE shuttle_files (
 id uuid PRIMARY KEY,user_id uuid NOT NULL REFERENCES shuttle_users(id),task_id uuid NOT NULL REFERENCES shuttle_tasks(id),
 telegram_id text NOT NULL,unique_id text NOT NULL,kind text NOT NULL,mime_type text NOT NULL,filename text NOT NULL,
 size_bytes bigint NOT NULL CHECK(size_bytes>=0),message_id bigint NOT NULL CHECK(message_id>0),sent_at timestamptz NOT NULL,
 accepted_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX shuttle_files_owner ON shuttle_files(user_id,id);
-- +goose Down
-- Export file provenance before intentional rollback; no file bytes are stored here.
DROP TABLE shuttle_files;
