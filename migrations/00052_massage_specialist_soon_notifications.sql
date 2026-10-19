-- +goose Up
ALTER TABLE shuttle_massage_specialist_notifications
 DROP CONSTRAINT shuttle_massage_specialist_notifications_kind_check;
ALTER TABLE shuttle_massage_specialist_notifications
 ADD CONSTRAINT shuttle_massage_specialist_notifications_kind_check CHECK(kind IN ('booked','cancelled','soon'));
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_massage_specialist_notifications IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_massage_specialist_notifications WHERE kind='soon') THEN
  RAISE EXCEPTION 'Export upcoming-session notification history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_massage_specialist_notifications
 DROP CONSTRAINT shuttle_massage_specialist_notifications_kind_check;
ALTER TABLE shuttle_massage_specialist_notifications
 ADD CONSTRAINT shuttle_massage_specialist_notifications_kind_check CHECK(kind IN ('booked','cancelled'));
