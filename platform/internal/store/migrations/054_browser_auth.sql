CREATE TABLE bot.browser_auth (
 id text PRIMARY KEY,
 browser_hash bytea NOT NULL,
 telegram_id bigint NOT NULL,
 language text NOT NULL,
 origin text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','approved','declined','cancelled')),
 created_at timestamptz NOT NULL DEFAULT now(),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '5 minutes',
 session_expires_at timestamptz,
 CHECK(length(id)=32)
);
CREATE INDEX browser_auth_recipient ON bot.browser_auth(telegram_id,created_at);
CREATE INDEX browser_auth_expiry ON bot.browser_auth(expires_at);
