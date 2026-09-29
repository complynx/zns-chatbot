CREATE TABLE bot.order_pages (
  owner text NOT NULL,
  scope text NOT NULL CHECK (scope IN ('orders','admin')),
  page integer NOT NULL DEFAULT 0 CHECK (page >= 0),
  last_update bigint NOT NULL DEFAULT 0,
  PRIMARY KEY(owner,scope)
);
CREATE TABLE bot.order_page_buttons (
  owner text NOT NULL,
  token text NOT NULL,
  scope text NOT NULL CHECK (scope IN ('orders','admin')),
  page integer NOT NULL CHECK (page >= 0),
  PRIMARY KEY(owner,token)
);
ALTER TABLE bot.order_cards ADD COLUMN visible boolean NOT NULL DEFAULT true;
