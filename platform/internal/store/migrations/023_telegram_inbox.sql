CREATE TABLE bot.telegram_inbox (
  update_id bigint PRIMARY KEY CHECK (update_id >= 0),
  payload jsonb NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now()
);
