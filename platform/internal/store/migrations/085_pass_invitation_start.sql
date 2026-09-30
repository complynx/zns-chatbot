-- New invitations start their own window after registration queue waits.
-- Historical/imported invitations have no reliable start evidence. Keep NULL:
-- the runtime preserves their existing created_at deadline rather than granting
-- every old invitation a new window during migration or import.
ALTER TABLE core.pass_bookings ADD COLUMN invitation_started_at timestamptz;
ALTER TABLE core.pass_bookings ADD CONSTRAINT pass_invitation_start_state
 CHECK(invitation_started_at IS NULL OR state='waiting-for-couple');
