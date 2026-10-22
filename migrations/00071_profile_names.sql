-- +goose Up
ALTER TABLE shuttle_user_profiles
 ADD COLUMN first_name text NOT NULL DEFAULT '' CHECK(char_length(first_name)<=200),
 ADD COLUMN last_name text NOT NULL DEFAULT '' CHECK(char_length(last_name)<=200),
 ADD COLUMN name_source text NOT NULL DEFAULT '' CHECK(name_source IN ('','user','iam')),
 ADD CONSTRAINT shuttle_profile_name_components CHECK(
  (first_name='' AND last_name='' AND name_source='') OR
  (first_name<>'' AND last_name<>'' AND name_source<>'' AND full_name=first_name||' '||last_name));

CREATE TABLE shuttle_profile_name_sync (
 user_id uuid PRIMARY KEY REFERENCES shuttle_user_profiles(user_id),
 desired_version bigint NOT NULL CHECK(desired_version>0),
 synced_version bigint NOT NULL DEFAULT 0 CHECK(synced_version>=0 AND synced_version<=desired_version),
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX shuttle_profile_name_sync_due ON shuttle_profile_name_sync(next_attempt_at,user_id);

-- +goose Down
LOCK TABLE shuttle_user_profiles,shuttle_profile_name_sync IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_user_profiles WHERE name_source<>'')
 OR EXISTS(SELECT 1 FROM shuttle_profile_name_sync) THEN
  RAISE EXCEPTION 'structured names and IAM sync state must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_profile_name_sync;
ALTER TABLE shuttle_user_profiles DROP CONSTRAINT shuttle_profile_name_components,
 DROP COLUMN first_name,DROP COLUMN last_name,DROP COLUMN name_source;
