-- +goose Up
CREATE TABLE shuttle_event_authorities (
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 role text NOT NULL CHECK(role IN ('none','ambassador','administrator')),
 version bigint NOT NULL CHECK(version>0),
 PRIMARY KEY(event_id,user_id)
);
CREATE TABLE shuttle_authority_operations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, request_hash bytea NOT NULL, result jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
CREATE TABLE shuttle_authority_preparations (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id text NOT NULL, proposal_id uuid NOT NULL UNIQUE, request_hash bytea NOT NULL,
 expires_at timestamptz NOT NULL, document jsonb NOT NULL,
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);
-- +goose Down
-- Export authority assignments, proposals and audit receipts before rollback.
DROP TABLE shuttle_authority_preparations;
DROP TABLE shuttle_authority_operations;
DROP TABLE shuttle_event_authorities;
