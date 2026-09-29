CREATE TABLE core.media (
  id text PRIMARY KEY, owner text NOT NULL REFERENCES core.users(id),
  filename text NOT NULL, mime text NOT NULL, body bytea NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL DEFAULT now() + interval '24 hours',
  CHECK(octet_length(body) BETWEEN 1 AND 20971520)
);
CREATE INDEX media_expiry ON core.media(expires_at);
