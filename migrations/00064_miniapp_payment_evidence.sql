-- +goose Up
-- Record first native acceptance without changing the shared original receipt.
CREATE TABLE shuttle_native_payment_evidence_actions (
 bot_id bigint NOT NULL,
 organization_id text NOT NULL,
 actor text NOT NULL,
 operation_id text NOT NULL,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 completed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id),
 FOREIGN KEY(bot_id,organization_id,actor,operation_id)
 REFERENCES shuttle_payment_operations(bot_id,organization_id,actor,operation_id)
);
CREATE INDEX shuttle_native_payment_evidence_actions_recent
 ON shuttle_native_payment_evidence_actions(user_id,completed_at DESC,operation_id DESC);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_native_payment_evidence_actions IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_native_payment_evidence_actions) THEN
  RAISE EXCEPTION 'Export native pass payment action history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_native_payment_evidence_actions;
