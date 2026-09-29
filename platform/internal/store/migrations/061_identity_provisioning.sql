-- The reservation survives a provider success followed by a local crash.
CREATE TABLE core.identity_provisioning (
  issuer text NOT NULL CHECK (issuer <> ''),
  organization text NOT NULL CHECK (organization <> ''),
  bot_id bigint NOT NULL CHECK (bot_id > 0 AND bot_id < 4503599627370496),
  telegram_id bigint NOT NULL CHECK (telegram_id > 0 AND telegram_id < 4503599627370496),
  owner text NOT NULL UNIQUE,
  subject text NOT NULL,
  operation text NOT NULL UNIQUE,
  first_name text NOT NULL,
  last_name text NOT NULL,
  language text NOT NULL,
  email text NOT NULL,
  ready boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (issuer, bot_id, telegram_id),
  UNIQUE (issuer, subject)
);
