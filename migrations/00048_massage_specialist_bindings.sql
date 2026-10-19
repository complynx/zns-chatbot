-- +goose Up
-- Public schedule IDs never grant authority without a private event binding.
-- Retain inactive rows so an event alias cannot silently move to another person.
CREATE TABLE shuttle_massage_specialist_bindings (
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 specialist_id text NOT NULL CHECK(specialist_id ~ '^[a-z][a-z0-9_-]{0,31}$'),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 active boolean NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 updated_by uuid NOT NULL REFERENCES shuttle_users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(event_id,specialist_id)
);
CREATE INDEX shuttle_massage_staff_events ON shuttle_massage_specialist_bindings(user_id,event_id)
 WHERE active;
CREATE TABLE shuttle_massage_binding_operations (
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 proposal_id uuid NOT NULL UNIQUE,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 specialist_id text NOT NULL CHECK(specialist_id ~ '^[a-z][a-z0-9_-]{0,31}$'),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 arguments jsonb NOT NULL CHECK(octet_length(arguments::text)<=16384),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT (clock_timestamp()+interval '5 minutes'),
 result jsonb CHECK(octet_length(result::text)<=32768),
 PRIMARY KEY(actor_id,operation_id)
);
CREATE TABLE shuttle_massage_binding_operation_aliases (
 actor_id uuid NOT NULL,
 operation_id uuid NOT NULL,
 canonical_operation_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_id,operation_id),
 FOREIGN KEY(actor_id,canonical_operation_id)
  REFERENCES shuttle_massage_binding_operations(actor_id,operation_id)
);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_massage_specialist_bindings,shuttle_massage_binding_operations,shuttle_massage_binding_operation_aliases IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_massage_specialist_bindings) OR
    EXISTS(SELECT 1 FROM shuttle_massage_binding_operations) OR
    EXISTS(SELECT 1 FROM shuttle_massage_binding_operation_aliases) THEN
  RAISE EXCEPTION 'Export massage specialist bindings and operation history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_massage_binding_operation_aliases;
DROP TABLE shuttle_massage_binding_operations;
DROP TABLE shuttle_massage_specialist_bindings;
