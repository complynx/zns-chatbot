-- +goose Up
CREATE TABLE shuttle_avatars (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 task_id uuid NOT NULL REFERENCES shuttle_tasks(id),
 delivery_id uuid NOT NULL UNIQUE REFERENCES shuttle_outbox(id),
 source_file_id uuid NOT NULL REFERENCES shuttle_files(id),
 operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 receipt jsonb NOT NULL,
 jpeg bytea NOT NULL CHECK(octet_length(jpeg)>0 AND octet_length(jpeg)<=4194304),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(user_id,operation_id)
);
CREATE INDEX shuttle_avatars_owner ON shuttle_avatars(user_id,id);

-- +goose Down
-- Stop avatar generation and export durable JPEGs and receipts before intentional removal.
-- Refuse concurrent use before checking history; retain the lock through DROP.
LOCK TABLE shuttle_avatars IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatars) THEN
  RAISE EXCEPTION 'avatar bytes and operation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_avatars;
