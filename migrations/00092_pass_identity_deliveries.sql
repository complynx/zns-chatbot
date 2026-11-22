-- +goose Up
CREATE TABLE shuttle_pass_identity_deliveries (
    user_id uuid NOT NULL REFERENCES shuttle_users(id),
    operation_id text NOT NULL,
    event_id uuid NOT NULL REFERENCES shuttle_events(id),
    event_version bigint NOT NULL,
    proposal_id uuid NOT NULL UNIQUE,
    recipient bigint NOT NULL CHECK (recipient > 0),
    expires_at timestamptz NOT NULL,
    state text NOT NULL CHECK (state IN ('prepared','delivery_unknown','sent','failed','too_large')),
    PRIMARY KEY(user_id,operation_id)
);

-- +goose Down
LOCK TABLE shuttle_pass_identity_deliveries IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_pass_identity_deliveries WHERE state <> 'prepared') THEN
        RAISE EXCEPTION 'preserve identity delivery receipts';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_pass_identity_deliveries;
