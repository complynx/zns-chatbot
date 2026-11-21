-- +goose Up
ALTER TABLE shuttle_registrations DROP CONSTRAINT shuttle_registrations_state_check;
ALTER TABLE shuttle_registrations ADD CHECK (state IN ('waitlisted','assigned','returned'));

CREATE TABLE shuttle_pass_returns (
    registration_id uuid PRIMARY KEY REFERENCES shuttle_registrations(id),
    actor_id uuid NOT NULL REFERENCES shuttle_users(id),
    operation_id uuid NOT NULL,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    returned_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    expected_version bigint NOT NULL,
    confirmed_minor bigint NOT NULL CHECK (confirmed_minor >= 0),
    refund_due_minor bigint NOT NULL CHECK (refund_due_minor BETWEEN 0 AND confirmed_minor),
    UNIQUE (actor_id,operation_id)
);
CREATE TABLE shuttle_pass_refunds (
    registration_id uuid PRIMARY KEY REFERENCES shuttle_pass_returns(registration_id),
    actor_id uuid NOT NULL REFERENCES shuttle_users(id),
    operation_id uuid NOT NULL,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    expected_version bigint NOT NULL,
    reason text NOT NULL CHECK (char_length(reason) BETWEEN 1 AND 500),
    recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (actor_id,operation_id)
);
-- +goose Down
LOCK TABLE shuttle_registrations, shuttle_pass_returns, shuttle_pass_refunds IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_pass_returns) THEN
        RAISE EXCEPTION 'pass return history requires explicit reconciliation before downgrade';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_pass_refunds;
DROP TABLE shuttle_pass_returns;
ALTER TABLE shuttle_registrations DROP CONSTRAINT shuttle_registrations_state_check;
ALTER TABLE shuttle_registrations ADD CHECK (state IN ('waitlisted','assigned'));
