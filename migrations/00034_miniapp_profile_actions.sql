-- +goose Up
CREATE TABLE shuttle_miniapp_profile_actions (
 user_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('set_name','set_language')),
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(user_id,operation_id) REFERENCES shuttle_profile_operations(user_id,operation_id)
);
CREATE INDEX shuttle_miniapp_profile_actions_recent ON shuttle_miniapp_profile_actions(user_id,completed_at DESC,operation_id);
-- +goose Down
-- Export retained manual action history before removing this journal.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_miniapp_profile_actions) THEN
  RAISE EXCEPTION 'Mini App profile action history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_profile_actions;
