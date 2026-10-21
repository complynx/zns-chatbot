-- +goose Up
ALTER TABLE shuttle_avatar_drafts ADD COLUMN original_expired_at timestamptz;
ALTER TABLE shuttle_avatar_drafts DROP CONSTRAINT shuttle_avatar_source_shape;
ALTER TABLE shuttle_avatar_drafts ADD CONSTRAINT shuttle_avatar_source_shape CHECK(
 (source_file_id IS NOT NULL AND history_record_id IS NULL AND history_archive_id IS NULL AND original IS NULL AND original_expired_at IS NULL)
 OR (source_file_id IS NULL AND history_record_id IS NOT NULL AND history_archive_id IS NOT NULL AND original IS NULL AND original_expired_at IS NULL)
 OR (source_file_id IS NULL AND history_record_id IS NULL AND history_archive_id IS NULL AND
  ((original IS NOT NULL AND octet_length(original)=source_size_bytes AND original_expired_at IS NULL)
   OR (original IS NULL AND original_expired_at IS NOT NULL)))
);
CREATE INDEX shuttle_avatar_retained_sources ON shuttle_avatar_drafts(user_id,created_at,id) WHERE original IS NOT NULL;

-- +goose Down
LOCK TABLE shuttle_avatar_drafts IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatar_drafts WHERE original_expired_at IS NOT NULL) THEN
  RAISE EXCEPTION 'expired avatar sources require explicit recovery before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP INDEX shuttle_avatar_retained_sources;
ALTER TABLE shuttle_avatar_drafts DROP CONSTRAINT shuttle_avatar_source_shape;
ALTER TABLE shuttle_avatar_drafts ADD CONSTRAINT shuttle_avatar_source_shape CHECK(
 (source_file_id IS NOT NULL AND history_record_id IS NULL AND history_archive_id IS NULL AND original IS NULL)
 OR (source_file_id IS NULL AND history_record_id IS NOT NULL AND history_archive_id IS NOT NULL AND original IS NULL)
 OR (source_file_id IS NULL AND history_record_id IS NULL AND history_archive_id IS NULL AND original IS NOT NULL AND octet_length(original)=source_size_bytes)
);
ALTER TABLE shuttle_avatar_drafts DROP COLUMN original_expired_at;
