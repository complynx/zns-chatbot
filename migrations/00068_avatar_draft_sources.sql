-- +goose Up
ALTER TABLE shuttle_avatar_drafts ADD COLUMN source_size_bytes bigint;
UPDATE shuttle_avatar_drafts SET source_size_bytes=octet_length(original)::bigint;
ALTER TABLE shuttle_avatar_drafts ALTER COLUMN source_size_bytes SET NOT NULL;
ALTER TABLE shuttle_avatar_drafts ADD CONSTRAINT shuttle_avatar_source_size CHECK(source_size_bytes>0 AND source_size_bytes<=20971520);
ALTER TABLE shuttle_avatar_drafts ADD COLUMN history_record_id uuid REFERENCES shuttle_legacy_history_records(id);
ALTER TABLE shuttle_avatar_drafts ADD COLUMN history_archive_id uuid REFERENCES shuttle_legacy_history_applications(archive_id);
ALTER TABLE shuttle_avatar_drafts ADD CONSTRAINT shuttle_avatar_history_membership
 FOREIGN KEY(history_archive_id,history_record_id) REFERENCES shuttle_legacy_history_memberships(archive_id,record_id);
ALTER TABLE shuttle_avatar_drafts ALTER COLUMN original DROP NOT NULL;
-- Source metadata is preserved before Telegram-backed originals are discarded.
UPDATE shuttle_avatar_drafts SET original=NULL WHERE source_file_id IS NOT NULL;
ALTER TABLE shuttle_avatar_drafts ADD CONSTRAINT shuttle_avatar_source_shape CHECK(
 (source_file_id IS NOT NULL AND history_record_id IS NULL AND history_archive_id IS NULL AND original IS NULL)
 OR (source_file_id IS NULL AND history_record_id IS NOT NULL AND history_archive_id IS NOT NULL AND original IS NULL)
 OR (source_file_id IS NULL AND history_record_id IS NULL AND history_archive_id IS NULL AND original IS NOT NULL AND octet_length(original)=source_size_bytes)
);
ALTER TABLE shuttle_avatar_draft_operations DROP CONSTRAINT shuttle_avatar_draft_operations_kind_check;
ALTER TABLE shuttle_avatar_draft_operations ADD CONSTRAINT shuttle_avatar_draft_operations_kind_check
 CHECK(kind IN ('create_upload','create_file','create_history','save_transform'));

-- +goose Down
LOCK TABLE shuttle_avatar_drafts,shuttle_avatar_draft_operations IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatar_drafts WHERE original IS NULL OR history_record_id IS NOT NULL OR history_archive_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM shuttle_avatar_draft_operations WHERE kind='create_history') THEN
  RAISE EXCEPTION 'reference-only avatar drafts require an explicit source recovery before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_avatar_draft_operations DROP CONSTRAINT shuttle_avatar_draft_operations_kind_check;
ALTER TABLE shuttle_avatar_draft_operations ADD CONSTRAINT shuttle_avatar_draft_operations_kind_check
 CHECK(kind IN ('create_upload','create_file','save_transform'));
ALTER TABLE shuttle_avatar_drafts DROP CONSTRAINT shuttle_avatar_source_shape;
ALTER TABLE shuttle_avatar_drafts ALTER COLUMN original SET NOT NULL;
ALTER TABLE shuttle_avatar_drafts DROP COLUMN history_record_id;
ALTER TABLE shuttle_avatar_drafts DROP COLUMN history_archive_id;
ALTER TABLE shuttle_avatar_drafts DROP CONSTRAINT shuttle_avatar_source_size;
ALTER TABLE shuttle_avatar_drafts DROP COLUMN source_size_bytes;
