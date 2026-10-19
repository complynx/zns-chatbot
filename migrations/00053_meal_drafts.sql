-- +goose Up
CREATE TABLE shuttle_meal_catalogs (
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 version bigint NOT NULL CHECK(version>0),
 document jsonb NOT NULL CHECK(octet_length(document::text)<=98304),
 PRIMARY KEY(event_id,version)
);
CREATE TABLE shuttle_meal_catalog_preparations (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL, event_id uuid NOT NULL REFERENCES shuttle_events(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 arguments jsonb NOT NULL CHECK(octet_length(arguments::text)<=98304),
 expires_at timestamptz NOT NULL, UNIQUE(user_id,operation_id)
);
CREATE TABLE shuttle_meal_catalog_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id), operation_id uuid NOT NULL,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=98304),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(user_id,operation_id)
);
CREATE TABLE shuttle_meal_drafts (
 user_id uuid NOT NULL REFERENCES shuttle_users(id), event_id uuid NOT NULL,
 catalog_version bigint NOT NULL, version bigint NOT NULL CHECK(version>0),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=98304),
 PRIMARY KEY(user_id,event_id),
 FOREIGN KEY(event_id,catalog_version) REFERENCES shuttle_meal_catalogs(event_id,version)
);
CREATE TABLE shuttle_meal_draft_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id), operation_id uuid NOT NULL,
 event_id uuid NOT NULL, catalog_version bigint NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=98304),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(event_id,catalog_version) REFERENCES shuttle_meal_catalogs(event_id,version)
);
CREATE TABLE shuttle_miniapp_meal_draft_actions (
 user_id uuid NOT NULL, operation_id uuid NOT NULL,
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(), PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(user_id,operation_id) REFERENCES shuttle_meal_draft_operations(user_id,operation_id)
);
CREATE INDEX shuttle_miniapp_meal_draft_actions_recent
 ON shuttle_miniapp_meal_draft_actions(user_id,completed_at DESC,operation_id);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_meal_catalogs,shuttle_meal_catalog_preparations,
 shuttle_meal_catalog_operations,shuttle_meal_drafts,shuttle_meal_draft_operations,
 shuttle_miniapp_meal_draft_actions IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_meal_catalogs)
 OR EXISTS(SELECT 1 FROM shuttle_meal_catalog_preparations)
 OR EXISTS(SELECT 1 FROM shuttle_meal_catalog_operations)
 OR EXISTS(SELECT 1 FROM shuttle_meal_drafts)
 OR EXISTS(SELECT 1 FROM shuttle_meal_draft_operations)
 OR EXISTS(SELECT 1 FROM shuttle_miniapp_meal_draft_actions) THEN
  RAISE EXCEPTION 'Meal catalogs, drafts and operation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_meal_draft_actions;
DROP TABLE shuttle_meal_draft_operations;
DROP TABLE shuttle_meal_drafts;
DROP TABLE shuttle_meal_catalog_operations;
DROP TABLE shuttle_meal_catalog_preparations;
DROP TABLE shuttle_meal_catalogs;
