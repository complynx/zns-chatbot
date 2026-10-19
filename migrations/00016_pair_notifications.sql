-- +goose Up
CREATE TABLE shuttle_pair_notifications (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL, organization_id text NOT NULL,
 invitation_id uuid NOT NULL REFERENCES shuttle_pair_invitations(id),
 invitation_version bigint NOT NULL CHECK(invitation_version>0),
 recipient_id uuid NOT NULL REFERENCES shuttle_users(id),
 kind text NOT NULL CHECK(kind IN ('pending','accepted','declined','revoked','expired')),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN
 ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked','dismissed')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 attempt_id uuid, payload jsonb, receipt jsonb NOT NULL DEFAULT '{}',
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(invitation_id,invitation_version,recipient_id)
);
CREATE INDEX shuttle_pair_notifications_ready ON shuttle_pair_notifications(next_attempt_at,id)
WHERE state IN ('pending','retryable');
-- +goose Down
-- Export notification intents and delivery outcomes before intentional rollback.
DROP TABLE shuttle_pair_notifications;
