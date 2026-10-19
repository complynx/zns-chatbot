-- +goose Up
CREATE TABLE shuttle_massage_schedules (
 event_id uuid PRIMARY KEY REFERENCES shuttle_events(id),
 version bigint NOT NULL CHECK(version>0),
 document jsonb NOT NULL CHECK(octet_length(document::text)<=98304)
);
CREATE TABLE shuttle_massage_schedule_preparations (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL, request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 arguments jsonb NOT NULL, expires_at timestamptz NOT NULL,
 UNIQUE(user_id,operation_id)
);
CREATE TABLE shuttle_massage_schedule_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id), operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32), result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(user_id,operation_id)
);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_massage_schedules)
 OR EXISTS(SELECT 1 FROM shuttle_massage_schedule_preparations)
 OR EXISTS(SELECT 1 FROM shuttle_massage_schedule_operations) THEN
  RAISE EXCEPTION 'massage schedules and approvals must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_massage_schedule_operations;
DROP TABLE shuttle_massage_schedule_preparations;
DROP TABLE shuttle_massage_schedules;
