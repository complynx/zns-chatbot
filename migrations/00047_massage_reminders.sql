-- +goose Up
-- Only new bookings create reminder intents. Historical backfill is explicit.
CREATE TABLE shuttle_massage_reminders (
 id uuid PRIMARY KEY,
 booking_id uuid NOT NULL REFERENCES shuttle_massage_bookings(id),
 kind text NOT NULL CHECK(kind IN ('owner_long','owner_short')),
 lead_seconds integer NOT NULL CHECK(lead_seconds>0),
 due_at timestamptz NOT NULL,
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN
 ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked','dismissed')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 attempt_id uuid,
 payload jsonb,
 receipt jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(booking_id,kind)
);
CREATE INDEX shuttle_massage_reminders_due ON shuttle_massage_reminders(due_at,id)
 WHERE state IN ('pending','retryable');
ALTER TABLE shuttle_notification_resolutions
 ADD COLUMN massage_reminder_id uuid REFERENCES shuttle_massage_reminders(id);
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int+(massage_reminder_id IS NOT NULL)::int=1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_massage_reminders IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_massage_reminders) THEN
  RAISE EXCEPTION 'Export massage reminder intents and delivery history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions DROP COLUMN massage_reminder_id;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int=1);
DROP TABLE shuttle_massage_reminders;
