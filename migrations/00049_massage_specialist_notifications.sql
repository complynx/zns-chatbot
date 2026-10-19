-- +goose Up
-- Preferences belong to the bot account and survive event grant changes.
CREATE TABLE shuttle_massage_specialist_preferences (
 user_id uuid PRIMARY KEY REFERENCES shuttle_users(id),
 notify_bookings boolean NOT NULL,
 version bigint NOT NULL CHECK(version>0)
);
CREATE TABLE shuttle_massage_specialist_preference_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 source text NOT NULL CHECK(source IN ('agent','miniapp')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=4096),
 PRIMARY KEY(user_id,operation_id)
);
-- Only new transitions capture a recipient and its authorization generations.
CREATE TABLE shuttle_massage_specialist_notifications (
 id uuid PRIMARY KEY,
 booking_id uuid NOT NULL REFERENCES shuttle_massage_bookings(id),
 kind text NOT NULL CHECK(kind IN ('booked','cancelled')),
 recipient_id uuid NOT NULL REFERENCES shuttle_users(id),
 binding_version bigint NOT NULL CHECK(binding_version>0),
 preference_version bigint NOT NULL CHECK(preference_version>=0),
 client_name text NOT NULL CHECK(char_length(client_name)<=256),
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
CREATE INDEX shuttle_massage_specialist_notifications_due
 ON shuttle_massage_specialist_notifications(next_attempt_at,id) WHERE state IN ('pending','retryable');
ALTER TABLE shuttle_notification_resolutions
 ADD COLUMN massage_specialist_notification_id uuid REFERENCES shuttle_massage_specialist_notifications(id);
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int+(massage_reminder_id IS NOT NULL)::int+
 (massage_specialist_notification_id IS NOT NULL)::int=1);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_massage_specialist_preferences,shuttle_massage_specialist_preference_operations,
 shuttle_massage_specialist_notifications IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_massage_specialist_preferences) OR
    EXISTS(SELECT 1 FROM shuttle_massage_specialist_preference_operations) OR
    EXISTS(SELECT 1 FROM shuttle_massage_specialist_notifications) THEN
  RAISE EXCEPTION 'Export massage specialist preferences, notices and operation history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_notification_resolutions DROP CONSTRAINT shuttle_resolution_one_target;
ALTER TABLE shuttle_notification_resolutions DROP COLUMN massage_specialist_notification_id;
ALTER TABLE shuttle_notification_resolutions ADD CONSTRAINT shuttle_resolution_one_target
 CHECK ((notification_id IS NOT NULL)::int+(pair_notification_id IS NOT NULL)::int+
 (order_notification_id IS NOT NULL)::int+(massage_reminder_id IS NOT NULL)::int=1);
DROP TABLE shuttle_massage_specialist_notifications;
DROP TABLE shuttle_massage_specialist_preference_operations;
DROP TABLE shuttle_massage_specialist_preferences;
