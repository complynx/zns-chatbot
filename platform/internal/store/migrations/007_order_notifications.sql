CREATE TABLE core.order_notifications (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  recipient text NOT NULL REFERENCES core.users(id),
  order_id text NOT NULL REFERENCES core.orders(id),
  payload jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  available_at timestamptz NOT NULL DEFAULT now(),
  attempted_at timestamptz,
  delivered_at timestamptz, failure text NOT NULL DEFAULT ''
);
CREATE INDEX order_notifications_pending ON core.order_notifications(available_at,id)
  WHERE delivered_at IS NULL AND failure='';
CREATE TABLE bot.notification_deliveries (
  id bigint PRIMARY KEY, message_id bigint NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
