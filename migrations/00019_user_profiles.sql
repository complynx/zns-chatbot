-- +goose Up
CREATE TABLE shuttle_user_profiles (
 user_id uuid PRIMARY KEY REFERENCES shuttle_users(id),
 version bigint NOT NULL CHECK(version >= 0),
 full_name text NOT NULL DEFAULT '' CHECK(char_length(full_name) <= 256),
 bot_language text NOT NULL DEFAULT 'auto' CHECK(bot_language IN ('auto','en','ru')),
 expectation_id uuid,
 expectation_created_at timestamptz,
 expectation_expires_at timestamptz,
 CHECK((expectation_id IS NULL AND expectation_created_at IS NULL AND expectation_expires_at IS NULL)
    OR (expectation_id IS NOT NULL AND expectation_created_at IS NOT NULL AND expectation_expires_at IS NOT NULL AND expectation_expires_at > expectation_created_at))
);
CREATE TABLE shuttle_profile_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL,
 PRIMARY KEY(user_id,operation_id)
);
-- +goose Down
-- Export user profiles and operation history before rollback.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_user_profiles) OR EXISTS(SELECT 1 FROM shuttle_profile_operations) THEN
  RAISE EXCEPTION 'user profiles and operation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_profile_operations;
DROP TABLE shuttle_user_profiles;
