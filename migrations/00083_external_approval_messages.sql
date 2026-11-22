-- +goose Up
CREATE TABLE shuttle_external_approval_messages (
    operation_id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES shuttle_users(id),
    message_id bigint,
    presentation_hash bytea NOT NULL CHECK (octet_length(presentation_hash) = 32),
    next_part integer NOT NULL DEFAULT 0 CHECK (next_part >= 0),
    delivery_attempt uuid NOT NULL,
    lease_until timestamptz NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX shuttle_external_approval_expiry ON shuttle_external_approval_messages(expires_at);

-- +goose Down
DROP TABLE shuttle_external_approval_messages;
