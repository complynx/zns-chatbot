-- +goose Up
ALTER TABLE shuttle_notification_resolutions ALTER COLUMN notification_id DROP NOT NULL;
ALTER TABLE shuttle_notification_resolutions ADD COLUMN pair_notification_id uuid REFERENCES shuttle_pair_notifications(id);
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int=1);
-- +goose Down
-- Export pair resolution history first. Refuse rollback while it is retained.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_notification_resolutions WHERE pair_notification_id IS NOT NULL) THEN
  RAISE EXCEPTION 'pair notification resolution history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions DROP COLUMN pair_notification_id;
ALTER TABLE shuttle_notification_resolutions ALTER COLUMN notification_id SET NOT NULL;
