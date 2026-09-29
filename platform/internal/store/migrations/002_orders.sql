CREATE TABLE core.order_events (
  id text PRIMARY KEY, deadline timestamptz NOT NULL,
  menu jsonb NOT NULL, extras jsonb NOT NULL
);
CREATE TABLE core.orders (
  id text PRIMARY KEY, event_id text NOT NULL REFERENCES core.order_events(id),
  owner text NOT NULL REFERENCES core.users(id), version bigint NOT NULL CHECK(version>0),
  choice jsonb NOT NULL, state text NOT NULL CHECK(state IN ('unpaid','proof','cash','paid','deleted')),
  attempt text NOT NULL DEFAULT '', attempt_at timestamptz,
  proof_file text NOT NULL DEFAULT '', payment_admin text NOT NULL DEFAULT '',
  country text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(), reminder_claimed_at timestamptz
);
CREATE INDEX orders_event_owner ON core.orders(event_id,owner,created_at);
CREATE TABLE core.order_admins (
  event_id text NOT NULL REFERENCES core.order_events(id),
  owner text NOT NULL REFERENCES core.users(id), country text NOT NULL CHECK(country IN ('be','ru')),
  PRIMARY KEY(event_id,owner)
);
CREATE TABLE core.order_operations (
  actor text NOT NULL REFERENCES core.users(id), key text NOT NULL,
  request_hash text NOT NULL, result jsonb NOT NULL, PRIMARY KEY(actor,key)
);
CREATE TABLE core.order_audit (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  order_id text NOT NULL REFERENCES core.orders(id), actor text NOT NULL,
  origin text NOT NULL, action text NOT NULL, version bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE core.order_notices (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  order_id text NOT NULL REFERENCES core.orders(id), owner text NOT NULL REFERENCES core.users(id),
  removed jsonb NOT NULL, old_total bigint NOT NULL, new_total bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), claimed_at timestamptz, sent_at timestamptz
);
