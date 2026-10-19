-- +goose Up
CREATE TABLE shuttle_miniapp_avatar_resolution_actions (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 proposal_id uuid NOT NULL REFERENCES shuttle_avatar_delivery_resolutions(proposal_id),
 kind text NOT NULL CHECK(kind IN ('prepare','confirm')),
 delivery_version bigint NOT NULL CHECK(delivery_version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id,kind)
);
CREATE INDEX shuttle_miniapp_avatar_resolution_actions_recent
 ON shuttle_miniapp_avatar_resolution_actions(user_id,created_at DESC,operation_id);

-- +goose Down
LOCK TABLE shuttle_miniapp_avatar_resolution_actions,shuttle_users,shuttle_avatar_delivery_resolutions IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_miniapp_avatar_resolution_actions) THEN
  RAISE EXCEPTION 'export and deliberately remove native avatar resolution actions before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_avatar_resolution_actions;
