CREATE TABLE bot.order_cards (
  owner text NOT NULL, card_key text NOT NULL, chat_id bigint NOT NULL,
  message_id bigint NOT NULL, view_hash text NOT NULL,
  PRIMARY KEY(owner,card_key)
);
CREATE TABLE bot.order_buttons (
  owner text NOT NULL, token text NOT NULL, command jsonb NOT NULL,
  PRIMARY KEY(owner,token)
);
