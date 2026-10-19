-- +goose Up
-- Account version is shared for CAS; delivery generations are per preference.
ALTER TABLE shuttle_massage_specialist_preferences
 ADD COLUMN notify_next boolean NOT NULL DEFAULT true,
 ADD COLUMN notify_bookings_version bigint NOT NULL DEFAULT 0 CHECK(notify_bookings_version>=0 AND notify_bookings_version<=version),
 ADD COLUMN notify_next_version bigint NOT NULL DEFAULT 0 CHECK(notify_next_version>=0 AND notify_next_version<=version);
UPDATE shuttle_massage_specialist_preferences SET notify_bookings_version=version;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_massage_specialist_preferences,shuttle_massage_specialist_preference_operations IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_massage_specialist_preferences WHERE NOT notify_next OR notify_next_version<>0 OR notify_bookings_version<>version) OR
    EXISTS(SELECT 1 FROM shuttle_massage_specialist_preference_operations WHERE result ? 'notify_next') THEN
  RAISE EXCEPTION 'Export upcoming-session preferences and operation history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_massage_specialist_preferences
 DROP COLUMN notify_next,DROP COLUMN notify_bookings_version,DROP COLUMN notify_next_version;
