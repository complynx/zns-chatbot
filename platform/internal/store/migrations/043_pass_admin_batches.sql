CREATE TABLE core.pass_admin_batches (
 actor text NOT NULL REFERENCES core.users(id),
 key_hash text NOT NULL,
 request_hash text NOT NULL,
 plan jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor,key_hash)
);
