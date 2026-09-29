-- Only a verified authorizer assertion may reserve an external subject.
CREATE TABLE core.identity_external_links (
  issuer text NOT NULL,
  bot_id bigint NOT NULL CHECK (bot_id > 0 AND bot_id < 4503599627370496),
  telegram_id bigint NOT NULL CHECK (telegram_id > 0 AND telegram_id < 4503599627370496),
  idp_id text NOT NULL CHECK (idp_id <> ''),
  external_subject text NOT NULL CHECK (external_subject <> ''),
  owner text NOT NULL REFERENCES core.users(id),
  subject text NOT NULL,
  ready boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (issuer, bot_id, telegram_id, idp_id),
  UNIQUE (issuer, idp_id, external_subject)
);
