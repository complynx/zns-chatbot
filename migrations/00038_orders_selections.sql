-- +goose Up
CREATE TABLE shuttle_orders_selections (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 version bigint NOT NULL CHECK(version>0),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=65536),
 PRIMARY KEY(user_id,event_id)
);
CREATE TABLE shuttle_orders_selection_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=65536),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id)
);
CREATE TABLE shuttle_miniapp_orders_selection_actions (
 user_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(user_id,operation_id) REFERENCES shuttle_orders_selection_operations(user_id,operation_id)
);
CREATE INDEX shuttle_miniapp_orders_selection_actions_recent
 ON shuttle_miniapp_orders_selection_actions(user_id,completed_at DESC,operation_id);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_orders_selections,shuttle_orders_selection_operations,
 shuttle_miniapp_orders_selection_actions IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_orders_selections)
 OR EXISTS(SELECT 1 FROM shuttle_orders_selection_operations)
 OR EXISTS(SELECT 1 FROM shuttle_miniapp_orders_selection_actions) THEN
  RAISE EXCEPTION 'Saved selections and operation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_orders_selection_actions;
DROP TABLE shuttle_orders_selection_operations;
DROP TABLE shuttle_orders_selections;
