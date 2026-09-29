ALTER TABLE bot.media_intake ADD COLUMN registration_command jsonb;
ALTER TABLE bot.media_intake ADD CONSTRAINT media_one_destination
  CHECK(command IS NULL OR registration_command IS NULL);
ALTER TABLE bot.media_buttons ADD COLUMN registration_event text NOT NULL DEFAULT '';
ALTER TABLE bot.media_buttons DROP CONSTRAINT media_buttons_owner_intake_id_action_order_id_version_key;
ALTER TABLE bot.media_buttons ADD UNIQUE(owner,intake_id,action,order_id,registration_event,version);
