-- +goose Up
CREATE TABLE shuttle_avatar_resolution_aliases (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id uuid NOT NULL, canonical_operation_id uuid NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id),
 FOREIGN KEY(bot_id,organization_id,actor,canonical_operation_id)
 REFERENCES shuttle_avatar_delivery_resolutions(bot_id,organization_id,actor,operation_id),
 CHECK(operation_id<>canonical_operation_id)
);

-- +goose Down
LOCK TABLE shuttle_avatar_resolution_aliases IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatar_resolution_aliases) THEN
  RAISE EXCEPTION 'export and deliberately remove avatar resolution aliases before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_avatar_resolution_aliases;
