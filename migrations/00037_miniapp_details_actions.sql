-- +goose Up
CREATE TABLE shuttle_miniapp_details_actions (
 user_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 field text NOT NULL CHECK(field IN ('legal_name','passport_number')),
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(user_id,operation_id) REFERENCES shuttle_registration_details_operations(user_id,operation_id)
);
CREATE INDEX shuttle_miniapp_details_actions_recent ON shuttle_miniapp_details_actions(user_id,completed_at DESC,operation_id);
-- +goose Down
-- Export retained manual action history before removing this journal.
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_miniapp_details_actions IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_miniapp_details_actions) THEN
  RAISE EXCEPTION 'Mini App registration details action history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_details_actions;
