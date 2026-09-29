-- Original imported/manual media has no model source. New agent selections
-- persist their evidence with the first winning command; old agent rows fail closed.
ALTER TABLE bot.media_intake ADD COLUMN command_source jsonb
    CHECK (command_source IS NULL OR
        (jsonb_typeof(command_source) = 'object' AND octet_length(command_source::text) <= 2097152));
