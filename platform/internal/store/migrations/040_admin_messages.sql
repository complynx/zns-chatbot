CREATE TABLE core.admin_messages (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor text NOT NULL REFERENCES core.users(id),
 key text NOT NULL,
 request jsonb NOT NULL,
 state text NOT NULL DEFAULT 'draft' CHECK(state IN ('draft','queued','cancelled')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(actor,key)
);
CREATE TABLE core.admin_message_deliveries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 message_id bigint NOT NULL REFERENCES core.admin_messages(id),
 destination jsonb NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sending','sent','failed','cancelled')),
 attempt bigint NOT NULL DEFAULT 0,
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 telegram_message_id bigint NOT NULL DEFAULT 0,
 failure text NOT NULL DEFAULT '',
 UNIQUE(message_id,destination)
);
CREATE INDEX admin_message_pending ON core.admin_message_deliveries(available_at,id)
 WHERE state IN ('pending','sending');
