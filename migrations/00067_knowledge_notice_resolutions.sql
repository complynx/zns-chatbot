-- +goose Up
ALTER TABLE shuttle_knowledge_notices DROP CONSTRAINT shuttle_knowledge_notices_state_check;
ALTER TABLE shuttle_knowledge_notices ADD CONSTRAINT shuttle_knowledge_notices_state_check
 CHECK(state IN ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked','dismissed'));
ALTER TABLE shuttle_notification_resolutions
 ADD COLUMN knowledge_notice_id uuid REFERENCES shuttle_knowledge_notices(id);
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int+(massage_reminder_id IS NOT NULL)::int+
 (massage_specialist_notification_id IS NOT NULL)::int+(knowledge_notice_id IS NOT NULL)::int=1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_notification_resolutions,shuttle_knowledge_notices IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_notification_resolutions WHERE knowledge_notice_id IS NOT NULL) OR
 EXISTS(SELECT 1 FROM shuttle_knowledge_notices WHERE state='dismissed') THEN
  RAISE EXCEPTION 'Export knowledge notice resolution history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions DROP COLUMN knowledge_notice_id;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int+(massage_reminder_id IS NOT NULL)::int+
 (massage_specialist_notification_id IS NOT NULL)::int=1);
ALTER TABLE shuttle_knowledge_notices DROP CONSTRAINT shuttle_knowledge_notices_state_check;
ALTER TABLE shuttle_knowledge_notices ADD CONSTRAINT shuttle_knowledge_notices_state_check
 CHECK(state IN ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked'));
