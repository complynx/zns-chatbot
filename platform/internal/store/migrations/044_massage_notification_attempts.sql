-- Persist recipient rotation so failed deliveries cannot starve later recipients.
CREATE TABLE bot.massage_notification_attempts (
 owner text PRIMARY KEY REFERENCES core.users(id),
 attempted_at timestamptz NOT NULL
);
