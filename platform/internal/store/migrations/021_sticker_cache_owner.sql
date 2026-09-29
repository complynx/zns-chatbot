-- Artwork interpretation is bot state, not authoritative business data.
-- Preserve cache contents while giving the bot its own schema boundary.
ALTER TABLE core.sticker_descriptions SET SCHEMA bot;
DO $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='zns_api') THEN
    REVOKE ALL ON bot.sticker_descriptions FROM zns_api;
  END IF;
  IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='zns_bot') THEN
    GRANT SELECT,INSERT,UPDATE,DELETE ON bot.sticker_descriptions TO zns_bot;
  END IF;
END $$;
