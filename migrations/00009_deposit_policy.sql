-- +goose Up
CREATE TABLE shuttle_event_deposit_policy (
 event_id uuid PRIMARY KEY REFERENCES shuttle_events(id), document jsonb
);
CREATE TABLE shuttle_pass_deposit_policy (
 pass_id uuid PRIMARY KEY REFERENCES shuttle_passes(id), document jsonb NOT NULL
);
-- +goose Down
-- Export frozen pass payment policies before an intentional data rollback.
DROP TABLE shuttle_pass_deposit_policy;
DROP TABLE shuttle_event_deposit_policy;
