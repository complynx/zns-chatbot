CREATE TABLE bot.pass_views (
  owner text PRIMARY KEY REFERENCES core.users(id), chat_id bigint NOT NULL,
  revision bigint NOT NULL, state jsonb NOT NULL,
  message_id bigint NOT NULL DEFAULT 0, view_hash text NOT NULL DEFAULT ''
);
CREATE TABLE bot.pass_buttons (
  owner text NOT NULL REFERENCES core.users(id), token text NOT NULL,
  revision bigint NOT NULL, action jsonb NOT NULL, PRIMARY KEY(owner,token)
);
CREATE TABLE bot.profile_buttons (
  owner text NOT NULL REFERENCES core.users(id), token text NOT NULL,
  action jsonb NOT NULL, PRIMARY KEY(owner,token)
);
