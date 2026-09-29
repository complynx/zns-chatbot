CREATE TABLE core.pass_notifications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_id text NOT NULL REFERENCES core.pass_events(id),
 owner text NOT NULL REFERENCES core.users(id),
 recipient text NOT NULL REFERENCES core.users(id),
 kind text NOT NULL,
 generation text NOT NULL,
 payload jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 delivered_at timestamptz,
 failure text NOT NULL DEFAULT '',
 UNIQUE(event_id,owner,recipient,kind,generation)
);
CREATE INDEX pass_notifications_pending ON core.pass_notifications(available_at,id) WHERE delivered_at IS NULL;
CREATE INDEX pass_notifications_recipient ON core.pass_notifications(recipient,id) WHERE delivered_at IS NULL;
CREATE TABLE core.pass_deadline_markers (
 event_id text NOT NULL, owner text NOT NULL, assigned_at timestamptz NOT NULL,
 first_at timestamptz NOT NULL, second_at timestamptz,
 PRIMARY KEY(event_id,owner,assigned_at),
 FOREIGN KEY(event_id,owner) REFERENCES core.pass_bookings(event_id,owner)
);
CREATE TABLE bot.pass_notification_deliveries (
 notice_id bigint PRIMARY KEY REFERENCES core.pass_notifications(id),
 message_id bigint NOT NULL
);
