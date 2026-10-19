-- +goose Up
CREATE TABLE shuttle_miniapp_assignment_actions (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id text NOT NULL CHECK(length(operation_id) BETWEEN 1 AND 128),
 proposal_id uuid NOT NULL REFERENCES shuttle_assignment_preparations(proposal_id),
 kind text NOT NULL CHECK(kind IN ('prepare','confirm')),
 registration_version bigint NOT NULL CHECK(registration_version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id,kind)
);
CREATE INDEX shuttle_miniapp_assignment_actions_recent
 ON shuttle_miniapp_assignment_actions(user_id,created_at DESC,operation_id);

-- +goose Down
LOCK TABLE shuttle_miniapp_assignment_actions,shuttle_users,shuttle_assignment_preparations IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_miniapp_assignment_actions) THEN
  RAISE EXCEPTION 'export and deliberately remove native assignment actions before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_assignment_actions;
