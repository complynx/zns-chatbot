-- Delivery intent can exist before runtime identity is configured. Unbound rows
-- require explicit bootstrap/import binding; no worker may adopt them.
ALTER TABLE core.admin_message_deliveries
 DROP CONSTRAINT admin_message_deliveries_state_check,
 ADD COLUMN bot_id bigint CHECK(bot_id>0),
 ADD COLUMN lease_until timestamptz,
 ADD COLUMN failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
 ADD COLUMN queued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 ADD CHECK(state IN ('pending','sending','sent','failed','cancelled','unknown','parked','paused'));
ALTER TABLE core.pass_registration_announcements
 ALTER COLUMN attempts TYPE bigint,
 DROP CONSTRAINT pass_registration_announcements_state_check,
 ADD COLUMN bot_id bigint CHECK(bot_id>0),
 ADD COLUMN lease_until timestamptz,
 ADD COLUMN failure_count bigint NOT NULL DEFAULT 0 CHECK(failure_count>=0),
 ADD COLUMN queued_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 ADD CHECK(state IN ('pending','sending','sent','failed','unknown','suppressed','cancelled','parked','paused'));
-- Existing sends have no trustworthy new-generation dispatch boundary.
UPDATE core.admin_message_deliveries SET state='unknown',failure='telegram_outcome_unknown' WHERE state='sending';
UPDATE core.pass_registration_announcements SET state='unknown',failure='telegram_outcome_unknown' WHERE state='sending';
CREATE INDEX admin_message_lane_head ON core.admin_message_deliveries
 (bot_id,(destination->>'chat'),(COALESCE(destination->>'thread','0')),id)
 WHERE state IN ('pending','sending','unknown','parked','paused');
CREATE INDEX registration_announcement_lane_head ON core.pass_registration_announcements
 (bot_id,channel,(COALESCE(thread_id,0)),id)
 WHERE state IN ('pending','sending','unknown','parked','paused');
CREATE TABLE core.delivery_pacing (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 chat text NOT NULL,
 not_before timestamptz NOT NULL DEFAULT '-infinity',
 pause_reason text NOT NULL DEFAULT '' CHECK(length(pause_reason)<=100),
 PRIMARY KEY(bot_id,chat)
);
-- Empty chat is the bot-wide pacing row. Payloads remain in domain outboxes.
DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_api','zns_runtime') LOOP
  EXECUTE format('GRANT SELECT,INSERT,UPDATE ON core.delivery_pacing TO %I',runtime_role);
 END LOOP;
END $$;

