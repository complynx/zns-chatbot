CREATE SCHEMA IF NOT EXISTS core;
CREATE SCHEMA IF NOT EXISTS bot;
CREATE TABLE IF NOT EXISTS core.users (
  id text PRIMARY KEY, telegram_id bigint UNIQUE NOT NULL, name text NOT NULL,
  can_book boolean NOT NULL DEFAULT false
);
CREATE TABLE IF NOT EXISTS core.slots (
  id text PRIMARY KEY, title text NOT NULL, capacity integer NOT NULL CHECK(capacity>0),
  price integer NOT NULL CHECK(price>=0), currency text NOT NULL,
  starts_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS core.workflows (
  owner text PRIMARY KEY REFERENCES core.users(id), slot_id text NOT NULL REFERENCES core.slots(id),
  version bigint NOT NULL CHECK(version>0), state text NOT NULL CHECK(state IN ('draft','booked','cancelled')),
  expires_at timestamptz NOT NULL
);
CREATE TABLE IF NOT EXISTS core.operations (
  owner text NOT NULL REFERENCES core.users(id), key text NOT NULL,
  request_hash text NOT NULL, result jsonb NOT NULL, PRIMARY KEY(owner,key)
);
CREATE TABLE IF NOT EXISTS core.audit (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  owner text NOT NULL REFERENCES core.users(id), origin text NOT NULL,
  action text NOT NULL, version bigint NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS core.outbox (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, owner text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS bot.interactions (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, owner text NOT NULL,
  update_id bigint NOT NULL, kind text NOT NULL, content jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(owner,update_id,kind)
);
CREATE INDEX IF NOT EXISTS interactions_owner_id ON bot.interactions(owner,id);
CREATE TABLE IF NOT EXISTS bot.messages (
  owner text PRIMARY KEY, chat_id bigint NOT NULL, message_id bigint NOT NULL,
  view_hash text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS bot.cursors (name text PRIMARY KEY, value bigint NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS bot.replies (
  update_id bigint PRIMARY KEY, plan jsonb NOT NULL
);
CREATE TABLE IF NOT EXISTS bot.fake_state (id boolean PRIMARY KEY DEFAULT true CHECK(id), data jsonb NOT NULL);
