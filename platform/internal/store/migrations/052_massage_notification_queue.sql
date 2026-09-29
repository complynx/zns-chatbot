-- Core owns recipient scheduling; retain the old adapter table for rollback.
CREATE TABLE core.massage_notification_attempts (
 owner text PRIMARY KEY REFERENCES core.users(id),
 attempted_at timestamptz NOT NULL
);
INSERT INTO core.massage_notification_attempts(owner,attempted_at)
SELECT owner,attempted_at FROM bot.massage_notification_attempts;
