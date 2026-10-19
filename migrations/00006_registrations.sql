-- +goose Up
CREATE TABLE shuttle_registrations (
 id uuid PRIMARY KEY, event_id uuid NOT NULL REFERENCES shuttle_events(id),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 role text NOT NULL CHECK(role IN ('leader','follower')),
 state text NOT NULL DEFAULT 'waitlisted' CHECK(state IN ('waitlisted','assigned')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 requested_at timestamptz NOT NULL,
 UNIQUE(event_id,user_id)
);
CREATE INDEX shuttle_registrations_queue ON shuttle_registrations(event_id,state,role,requested_at,id);
CREATE TABLE shuttle_passes (
 id uuid PRIMARY KEY, registration_id uuid NOT NULL UNIQUE REFERENCES shuttle_registrations(id),
 price_minor bigint NOT NULL CHECK(price_minor BETWEEN 0 AND 9007199254740991),
 currency text NOT NULL CHECK(currency ~ '^[A-Z]{3}$'), currency_scale integer NOT NULL CHECK(currency_scale BETWEEN 0 AND 4),
 assigned_at timestamptz NOT NULL, payment_due_at timestamptz NOT NULL CHECK(payment_due_at>assigned_at)
);
CREATE TABLE shuttle_registration_operations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, request_hash bytea NOT NULL, result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Export registrations, passes and receipts before an intentional data rollback.
DROP TABLE shuttle_registration_operations;
DROP TABLE shuttle_passes;
DROP TABLE shuttle_registrations;
