-- +goose Up
CREATE TABLE shuttle_pass_promo_choices (
    event_id uuid NOT NULL REFERENCES shuttle_events(id),
    user_id uuid NOT NULL REFERENCES shuttle_users(id),
    tier_id text NOT NULL,
    selected_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL CHECK (expires_at > selected_at),
    sequence bigint GENERATED ALWAYS AS IDENTITY,
    forfeited boolean NOT NULL DEFAULT false,
    awarded_role text CHECK (awarded_role IN ('leader','follower')),
    tier_number integer,
    price_minor bigint,
    PRIMARY KEY(event_id,user_id,tier_id),
    CHECK ((awarded_role IS NULL AND tier_number IS NULL AND price_minor IS NULL) OR
           (awarded_role IS NOT NULL AND tier_number IS NOT NULL AND price_minor IS NOT NULL AND NOT forfeited))
);
CREATE INDEX shuttle_promo_choices_order ON shuttle_pass_promo_choices(event_id,tier_id,selected_at,sequence);
CREATE UNIQUE INDEX shuttle_promo_one_win ON shuttle_pass_promo_choices(event_id,user_id) WHERE awarded_role IS NOT NULL;
CREATE TABLE shuttle_pass_assignment_notices (
    pass_id uuid PRIMARY KEY REFERENCES shuttle_passes(id),
    state text NOT NULL DEFAULT 'pending',
    version bigint NOT NULL DEFAULT 1,
    next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    receipt jsonb
);

-- +goose Down
LOCK TABLE shuttle_pass_promo_choices, shuttle_pass_assignment_notices IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_pass_promo_choices) OR EXISTS (SELECT 1 FROM shuttle_pass_assignment_notices) THEN
        RAISE EXCEPTION 'pass sale history exists';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_pass_assignment_notices;
DROP TABLE shuttle_pass_promo_choices;
