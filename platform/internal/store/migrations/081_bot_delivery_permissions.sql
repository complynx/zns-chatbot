-- The split bot owns transport metadata, while private domain payloads stay
-- behind the authenticated application boundary.
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zns_bot') THEN
  GRANT USAGE ON SCHEMA core TO zns_bot;
  GRANT SELECT, INSERT, UPDATE ON
   core.delivery_pacing, core.delivery_lanes,
   core.delivery_queue, core.delivery_fairness TO zns_bot;
 END IF;
END $$;
