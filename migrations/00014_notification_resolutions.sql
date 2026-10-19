-- +goose Up
CREATE TABLE shuttle_notification_resolutions (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, proposal_id uuid NOT NULL UNIQUE,
 notification_id uuid NOT NULL REFERENCES shuttle_payment_notifications(id),
 request_hash bytea NOT NULL, expires_at timestamptz NOT NULL,
 document jsonb NOT NULL, result jsonb,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Binary rollback retains proposals and receipts. Export before intentional schema rollback.
DROP TABLE shuttle_notification_resolutions;
