-- +goose Up
CREATE TABLE shuttle_payment_notifications (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL, organization_id text NOT NULL,
 evidence_id uuid NOT NULL REFERENCES shuttle_payment_evidence(id),
 recipient_id uuid NOT NULL REFERENCES shuttle_users(id),
 kind text NOT NULL CHECK(kind IN ('submitted','confirmed','rejected')),
 document jsonb NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN
 ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked','dismissed')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 attempt_id uuid, payload jsonb, receipt jsonb NOT NULL DEFAULT '{}',
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(evidence_id,recipient_id,kind)
);
CREATE INDEX shuttle_payment_notifications_ready ON shuttle_payment_notifications(next_attempt_at,id)
WHERE state IN ('pending','retryable');
-- +goose Down
-- Export notification documents and delivery outcomes before intentional rollback.
DROP TABLE shuttle_payment_notifications;
