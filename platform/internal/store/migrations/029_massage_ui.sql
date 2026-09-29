CREATE TABLE bot.massage_views (
  owner text PRIMARY KEY REFERENCES core.users(id), chat_id bigint NOT NULL,
  revision bigint NOT NULL, state jsonb NOT NULL,
  message_id bigint NOT NULL DEFAULT 0, view_hash text NOT NULL DEFAULT ''
);
CREATE TABLE bot.massage_buttons (
  owner text NOT NULL REFERENCES core.users(id), token text NOT NULL,
  revision bigint NOT NULL, action jsonb NOT NULL, PRIMARY KEY(owner,token)
);
CREATE TABLE bot.massage_deliveries (
  notice_id bigint PRIMARY KEY REFERENCES core.massage_notices(id), message_id bigint NOT NULL
);
