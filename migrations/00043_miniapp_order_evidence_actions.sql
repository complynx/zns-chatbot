-- +goose Up
-- Native acceptance history references immutable receipts without copying money or files.
CREATE TABLE shuttle_miniapp_order_evidence_actions (
 user_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 kind text NOT NULL DEFAULT 'submit' CHECK(kind='submit'),
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(user_id,operation_id,kind) REFERENCES shuttle_order_evidence_operations(user_id,operation_id,kind)
);
CREATE INDEX shuttle_miniapp_order_evidence_actions_recent ON shuttle_miniapp_order_evidence_actions(user_id,completed_at DESC,operation_id DESC);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_miniapp_order_evidence_actions IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_miniapp_order_evidence_actions) THEN
  RAISE EXCEPTION 'Export native order proof action history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_order_evidence_actions;
