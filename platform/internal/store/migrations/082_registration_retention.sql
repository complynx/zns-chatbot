ALTER TABLE core.registration_intents
 ADD COLUMN effective_position bigint,
 ADD COLUMN turn_expires_at timestamptz,
 ADD COLUMN requeue_count bigint NOT NULL DEFAULT 0 CHECK (requeue_count>=0);

-- Initial capture and later turns share the existing ingress ordering allocator.
-- A consumed sequence value is a queue rank, not a fabricated ingress record.
UPDATE core.registration_intents SET effective_position=ingress_id
 WHERE origin='canonical_ingress';
CREATE INDEX registration_intents_turn ON core.registration_intents(event_id,turn_expires_at)
 WHERE state='captured';
