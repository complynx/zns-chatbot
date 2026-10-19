-- +goose Up
ALTER TABLE shuttle_notification_resolutions
 ADD COLUMN order_notification_id uuid REFERENCES shuttle_order_evidence_notices(id);
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int=1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_notification_resolutions IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_notification_resolutions WHERE order_notification_id IS NOT NULL) THEN
  RAISE EXCEPTION 'Export order notice resolution history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions DROP COLUMN order_notification_id;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int=1);
