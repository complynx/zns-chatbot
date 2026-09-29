-- Existing intent remains unbound until explicit bootstrap/import assigns a bot.
-- Domain payloads and business eligibility remain owned by each outbox.
ALTER TABLE core.order_notifications
 ADD COLUMN bot_id bigint CHECK(bot_id>0),
 ADD COLUMN delivery_state text NOT NULL DEFAULT 'pending' CHECK(delivery_state IN ('pending','sending','sent','failed','cancelled','unknown','parked','paused')),
 ADD COLUMN delivery_chat bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_attempt bigint NOT NULL DEFAULT 0 CHECK(delivery_attempt>=0),
 ADD COLUMN lease_until timestamptz,
 ADD COLUMN telegram_message_id bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_text text NOT NULL DEFAULT '',
 ADD COLUMN failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
 ADD COLUMN followup_pending boolean NOT NULL DEFAULT false,
 ADD COLUMN followup_failure text NOT NULL DEFAULT '' CHECK(length(followup_failure)<=100),
 ADD COLUMN followup_attempts bigint NOT NULL DEFAULT 0 CHECK(followup_attempts>=0);
UPDATE core.order_notifications SET delivery_state=CASE WHEN failure='' THEN 'sent' ELSE 'failed' END WHERE delivered_at IS NOT NULL;
CREATE INDEX order_notifications_delivery_lane ON core.order_notifications(bot_id,recipient,id)
 WHERE delivery_state IN ('pending','sending','unknown','parked','paused') OR followup_pending;

ALTER TABLE core.pass_notifications
 ADD COLUMN bot_id bigint CHECK(bot_id>0),
 ADD COLUMN delivery_state text NOT NULL DEFAULT 'pending' CHECK(delivery_state IN ('pending','sending','sent','failed','cancelled','unknown','parked','paused')),
 ADD COLUMN delivery_chat bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_attempt bigint NOT NULL DEFAULT 0 CHECK(delivery_attempt>=0),
 ADD COLUMN lease_until timestamptz,
 ADD COLUMN telegram_message_id bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_text text NOT NULL DEFAULT '',
 ADD COLUMN failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
 ADD COLUMN followup_pending boolean NOT NULL DEFAULT false,
 ADD COLUMN followup_failure text NOT NULL DEFAULT '' CHECK(length(followup_failure)<=100),
 ADD COLUMN followup_attempts bigint NOT NULL DEFAULT 0 CHECK(followup_attempts>=0);
UPDATE core.pass_notifications SET delivery_state=CASE WHEN failure='' THEN 'sent' ELSE 'failed' END WHERE delivered_at IS NOT NULL;
CREATE INDEX pass_notifications_delivery_lane ON core.pass_notifications(bot_id,recipient,id)
 WHERE delivery_state IN ('pending','sending','unknown','parked','paused') OR followup_pending;

ALTER TABLE core.massage_notices
 ADD COLUMN bot_id bigint CHECK(bot_id>0),
 ADD COLUMN delivery_state text NOT NULL DEFAULT 'pending' CHECK(delivery_state IN ('pending','sending','sent','failed','cancelled','unknown','parked','paused')),
 ADD COLUMN delivery_chat bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_attempt bigint NOT NULL DEFAULT 0 CHECK(delivery_attempt>=0),
 ADD COLUMN lease_until timestamptz,
 ADD COLUMN telegram_message_id bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_text text NOT NULL DEFAULT '',
 ADD COLUMN failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
 ADD COLUMN followup_pending boolean NOT NULL DEFAULT false,
 ADD COLUMN followup_failure text NOT NULL DEFAULT '' CHECK(length(followup_failure)<=100),
 ADD COLUMN followup_attempts bigint NOT NULL DEFAULT 0 CHECK(followup_attempts>=0);
ALTER TABLE core.massage_notices ADD COLUMN available_at timestamptz NOT NULL DEFAULT clock_timestamp(), ADD COLUMN failure text NOT NULL DEFAULT '';
UPDATE core.massage_notices SET delivery_state=CASE WHEN failure='' THEN 'sent' ELSE 'failed' END WHERE sent_at IS NOT NULL;
CREATE INDEX massage_notices_delivery_lane ON core.massage_notices(bot_id,owner,id)
 WHERE delivery_state IN ('pending','sending','unknown','parked','paused') OR followup_pending;

ALTER TABLE core.food_notifications
 ADD COLUMN bot_id bigint CHECK(bot_id>0),
 ADD COLUMN delivery_state text NOT NULL DEFAULT 'pending' CHECK(delivery_state IN ('pending','sending','sent','failed','cancelled','unknown','parked','paused')),
 ADD COLUMN delivery_chat bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_attempt bigint NOT NULL DEFAULT 0 CHECK(delivery_attempt>=0),
 ADD COLUMN lease_until timestamptz,
 ADD COLUMN telegram_message_id bigint NOT NULL DEFAULT 0,
 ADD COLUMN delivery_text text NOT NULL DEFAULT '',
 ADD COLUMN failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
 ADD COLUMN followup_pending boolean NOT NULL DEFAULT false,
 ADD COLUMN followup_failure text NOT NULL DEFAULT '' CHECK(length(followup_failure)<=100),
 ADD COLUMN followup_attempts bigint NOT NULL DEFAULT 0 CHECK(followup_attempts>=0);
ALTER TABLE core.food_notifications ADD COLUMN available_at timestamptz NOT NULL DEFAULT clock_timestamp(), ADD COLUMN failure text NOT NULL DEFAULT '';
UPDATE core.food_notifications SET delivery_state=CASE WHEN failure='' THEN 'sent' ELSE 'failed' END WHERE sent_at IS NOT NULL OR imported_sent;
CREATE INDEX food_notifications_delivery_lane ON core.food_notifications(bot_id,owner,id)
 WHERE delivery_state IN ('pending','sending','unknown','parked','paused') OR followup_pending;
