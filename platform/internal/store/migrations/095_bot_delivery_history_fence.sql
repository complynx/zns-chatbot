-- The split delivery worker locks history metadata before saving a wire.
-- PostgreSQL requires UPDATE privilege for FOR UPDATE; private history columns
-- remain outside the bot role's grants.
DO $$
BEGIN
 IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'zns_bot') THEN
  GRANT INSERT(owner), SELECT(owner,version), UPDATE(version)
   ON core.conversation_summaries TO zns_bot;
 END IF;
END $$;
