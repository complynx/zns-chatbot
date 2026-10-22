-- +goose Up
CREATE TABLE shuttle_history_account_receipts (
 record_id uuid PRIMARY KEY REFERENCES shuttle_legacy_history_records(id),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 receipt jsonb NOT NULL
);

-- +goose Down
LOCK TABLE shuttle_history_account_receipts IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_history_account_receipts) THEN
  RAISE EXCEPTION 'export account import receipts before rollback; imported users are not automatically deleted';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_history_account_receipts;
