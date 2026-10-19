-- +goose Up
CREATE TABLE shuttle_event_registration_rules (
 event_id uuid PRIMARY KEY REFERENCES shuttle_events(id),
 document jsonb CHECK(document IS NULL OR jsonb_typeof(document)='object')
);

-- +goose Down
DROP TABLE shuttle_event_registration_rules;
