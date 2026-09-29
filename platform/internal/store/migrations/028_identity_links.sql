CREATE TABLE core.zitadel_identities (
  owner text PRIMARY KEY REFERENCES core.users(id),
  issuer text NOT NULL,
  subject text NOT NULL,
  active boolean NOT NULL DEFAULT true,
  UNIQUE(issuer,subject),
  CHECK(issuer <> '' AND subject <> '')
);
CREATE TABLE core.telegram_identities (
  bot_id bigint NOT NULL CHECK(bot_id > 0 AND bot_id < 4503599627370496),
  telegram_id bigint NOT NULL CHECK(telegram_id > 0 AND telegram_id < 4503599627370496),
  owner text NOT NULL REFERENCES core.zitadel_identities(owner),
  PRIMARY KEY(bot_id,telegram_id),
  UNIQUE(bot_id,owner)
);
