-- +goose Up
CREATE TABLE shuttle_user_bans (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL REFERENCES shuttle_users(id),
    actor text NOT NULL CHECK (btrim(actor) <> ''),
    reason text NOT NULL CHECK (btrim(reason) <> ''),
    request_hash bytea NOT NULL CHECK (octet_length(request_hash) = 32),
    previously_revoked boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (user_id, request_hash)
);

-- +goose Down
-- Ban audit is durable even if an application release is rolled back.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_user_bans) THEN
        RAISE EXCEPTION 'user ban audit must be preserved; use a reviewed forward migration';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_user_bans;
