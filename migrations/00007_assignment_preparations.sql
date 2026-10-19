-- +goose Up
CREATE TABLE shuttle_assignment_preparations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, proposal_id uuid NOT NULL UNIQUE,
 request_hash bytea NOT NULL, expires_at timestamptz NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Binary rollback keeps proposals and registration receipts; do not drop these for a binary rollback.
DROP TABLE shuttle_assignment_preparations;
