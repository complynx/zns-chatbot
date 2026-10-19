-- +goose Up
ALTER TABLE shuttle_tasks
 ADD COLUMN response_started_at timestamptz,
 ADD COLUMN response_first_chunk_seq integer,
 ADD COLUMN response_last_chunk_seq integer,
 ADD COLUMN response_delivered_at timestamptz,
 ADD CONSTRAINT shuttle_response_range CHECK (
  (response_first_chunk_seq IS NULL AND response_last_chunk_seq IS NULL)
  OR (response_started_at IS NOT NULL AND response_first_chunk_seq IS NOT NULL
      AND response_last_chunk_seq IS NOT NULL AND response_first_chunk_seq>=0
      AND response_last_chunk_seq>=response_first_chunk_seq)),
 ADD CONSTRAINT shuttle_response_delivered CHECK (
  response_delivered_at IS NULL OR
  (response_started_at IS NOT NULL AND response_first_chunk_seq IS NOT NULL
   AND response_last_chunk_seq IS NOT NULL));

-- +goose Down
-- Preserve accepted measurement facts on an executable rollback.
LOCK TABLE shuttle_tasks IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_tasks WHERE response_started_at IS NOT NULL
 OR response_first_chunk_seq IS NOT NULL OR response_last_chunk_seq IS NOT NULL
 OR response_delivered_at IS NOT NULL) THEN
  RAISE EXCEPTION 'response timing facts must be exported before schema rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_tasks DROP CONSTRAINT shuttle_response_delivered,
 DROP CONSTRAINT shuttle_response_range,
 DROP COLUMN response_delivered_at, DROP COLUMN response_last_chunk_seq,
 DROP COLUMN response_first_chunk_seq, DROP COLUMN response_started_at;
