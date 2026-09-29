CREATE TABLE core.order_proofs (
  id text PRIMARY KEY, owner text NOT NULL REFERENCES core.users(id),
  filename text NOT NULL, body bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  CHECK(octet_length(body) BETWEEN 1 AND 20971520)
);
CREATE TABLE bot.proof_pending (
  owner text PRIMARY KEY, selected_update bigint NOT NULL, command jsonb NOT NULL
);
CREATE TABLE bot.proof_submissions (
  update_id bigint PRIMARY KEY, owner text NOT NULL, command jsonb NOT NULL
);
CREATE TABLE bot.proof_sources (
  proof_id text PRIMARY KEY, chat_id bigint NOT NULL, message_id bigint NOT NULL
);
CREATE TABLE bot.fake_files (
  id text PRIMARY KEY, filename text NOT NULL, body bytea NOT NULL
);
