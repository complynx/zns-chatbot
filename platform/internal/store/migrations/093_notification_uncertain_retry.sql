-- Keep the last unknown wire fact separate from the owner's retry state.
-- Existing unknown rows record adoption time, not a historical transport time.
ALTER TABLE core.order_notifications
 ADD COLUMN last_uncertain_attempt bigint,
 ADD COLUMN last_uncertain_reason text,
 ADD COLUMN last_uncertain_recorded_at timestamptz,
 ADD COLUMN uncertain_resends bigint NOT NULL DEFAULT 0 CHECK(uncertain_resends BETWEEN 0 AND 3);
UPDATE core.order_notifications SET last_uncertain_attempt=delivery_attempt,
 last_uncertain_reason='telegram_outcome_unknown',last_uncertain_recorded_at=clock_timestamp()
 WHERE delivery_state='unknown';
CREATE INDEX order_notifications_recoverable ON core.order_notifications(bot_id,id)
 WHERE delivery_state IN ('sending','unknown');

ALTER TABLE core.food_notifications
 ADD COLUMN last_uncertain_attempt bigint,
 ADD COLUMN last_uncertain_reason text,
 ADD COLUMN last_uncertain_recorded_at timestamptz,
 ADD COLUMN uncertain_resends bigint NOT NULL DEFAULT 0 CHECK(uncertain_resends BETWEEN 0 AND 3);
UPDATE core.food_notifications SET last_uncertain_attempt=delivery_attempt,
 last_uncertain_reason='telegram_outcome_unknown',last_uncertain_recorded_at=clock_timestamp()
 WHERE delivery_state='unknown';
CREATE INDEX food_notifications_recoverable ON core.food_notifications(bot_id,id)
 WHERE delivery_state IN ('sending','unknown');

ALTER TABLE core.massage_notices
 ADD COLUMN last_uncertain_attempt bigint,
 ADD COLUMN last_uncertain_reason text,
 ADD COLUMN last_uncertain_recorded_at timestamptz,
 ADD COLUMN uncertain_resends bigint NOT NULL DEFAULT 0 CHECK(uncertain_resends BETWEEN 0 AND 3);
UPDATE core.massage_notices SET last_uncertain_attempt=delivery_attempt,
 last_uncertain_reason='telegram_outcome_unknown',last_uncertain_recorded_at=clock_timestamp()
 WHERE delivery_state='unknown';
CREATE INDEX massage_notices_recoverable ON core.massage_notices(bot_id,id)
 WHERE delivery_state IN ('sending','unknown');

ALTER TABLE core.pass_notifications
 ADD COLUMN last_uncertain_attempt bigint,
 ADD COLUMN last_uncertain_reason text,
 ADD COLUMN last_uncertain_recorded_at timestamptz,
 ADD COLUMN uncertain_resends bigint NOT NULL DEFAULT 0 CHECK(uncertain_resends BETWEEN 0 AND 3);
UPDATE core.pass_notifications SET last_uncertain_attempt=delivery_attempt,
 last_uncertain_reason='telegram_outcome_unknown',last_uncertain_recorded_at=clock_timestamp()
 WHERE delivery_state='unknown';
CREATE INDEX pass_notifications_recoverable ON core.pass_notifications(bot_id,id)
 WHERE delivery_state IN ('sending','unknown');
