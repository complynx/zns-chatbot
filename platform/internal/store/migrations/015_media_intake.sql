CREATE TABLE bot.media_intake (
  id text PRIMARY KEY, owner text NOT NULL, update_id bigint NOT NULL UNIQUE,
  attachment_id text NOT NULL, status text NOT NULL DEFAULT 'new',
  notice text NOT NULL DEFAULT '', model_text text NOT NULL DEFAULT '',
  command jsonb, created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',
  CHECK(status IN ('new','purpose','choose','done'))
);
CREATE TABLE bot.media_buttons (
  token text PRIMARY KEY, owner text NOT NULL,
  intake_id text NOT NULL REFERENCES bot.media_intake(id) ON DELETE CASCADE,
  action text NOT NULL, order_id text NOT NULL DEFAULT '', version bigint NOT NULL DEFAULT 0,
  UNIQUE(owner,intake_id,action,order_id,version)
);
