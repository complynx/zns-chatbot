-- Retain the last observed uncertainty, not a reconstructed wire history.
-- Resends count admissions after uncertainty; an admitted call may cross the wire.
ALTER TABLE core.admin_message_deliveries
 ADD COLUMN content_captured boolean NOT NULL DEFAULT true,
 ADD COLUMN last_confirmed_attempt bigint CHECK(last_confirmed_attempt>0),
 ADD COLUMN last_uncertain_attempt bigint CHECK(last_uncertain_attempt>=0),
 ADD COLUMN last_uncertain_reason text CHECK(length(last_uncertain_reason) BETWEEN 1 AND 100),
 ADD COLUMN last_uncertain_recorded_at timestamptz,
 ADD COLUMN uncertain_resends bigint NOT NULL DEFAULT 0 CHECK(uncertain_resends BETWEEN 0 AND 3),
 ADD CHECK(num_nonnulls(last_uncertain_attempt,last_uncertain_reason,last_uncertain_recorded_at) IN (0,3));
-- Existing NULL content is historical absence, not a known lost capture.
-- New deliveries expect stored content; a later NULL must never use fallback.
UPDATE core.admin_message_deliveries SET content_captured=false WHERE content IS NULL;
ALTER TABLE core.pass_registration_announcements
 ADD COLUMN rendered_text text,
 ADD COLUMN rendered_admitted boolean NOT NULL DEFAULT false CHECK(NOT rendered_admitted OR rendered_text IS NOT NULL),
 ADD COLUMN last_confirmed_attempt bigint CHECK(last_confirmed_attempt>0),
 ADD COLUMN last_uncertain_attempt bigint CHECK(last_uncertain_attempt>=0),
 ADD COLUMN last_uncertain_reason text CHECK(length(last_uncertain_reason) BETWEEN 1 AND 100),
 ADD COLUMN last_uncertain_recorded_at timestamptz,
 ADD COLUMN uncertain_resends bigint NOT NULL DEFAULT 0 CHECK(uncertain_resends BETWEEN 0 AND 3),
 ADD CHECK(num_nonnulls(last_uncertain_attempt,last_uncertain_reason,last_uncertain_recorded_at) IN (0,3));
