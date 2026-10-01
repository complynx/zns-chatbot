ALTER TABLE bot.delivery_intents
    ADD COLUMN last_uncertain_attempt bigint CHECK (last_uncertain_attempt > 0),
    ADD COLUMN last_uncertain_reason text,
    ADD COLUMN last_uncertain_recorded_at timestamptz,
    ADD COLUMN uncertain_resends smallint NOT NULL DEFAULT 0 CHECK (uncertain_resends BETWEEN 0 AND 3),
    ADD COLUMN wire_capture_key text,
    ADD COLUMN wire_capture_hash text;

-- Historical rows keep unknown metadata absent until an actual recovery observation.
ALTER TABLE bot.delivery_intents ADD CONSTRAINT bot_delivery_uncertain_metadata_complete CHECK (
    (last_uncertain_attempt IS NULL AND last_uncertain_reason IS NULL AND last_uncertain_recorded_at IS NULL)
    OR (last_uncertain_attempt IS NOT NULL AND last_uncertain_reason IS NOT NULL AND last_uncertain_recorded_at IS NOT NULL)
);

ALTER TABLE bot.delivery_intents ADD CONSTRAINT bot_delivery_wire_capture_complete CHECK (
    (wire_capture_key IS NULL AND wire_capture_hash IS NULL)
    OR (wire_capture_key IS NOT NULL AND wire_capture_hash IS NOT NULL)
);

-- Wire bodies share private history deletion with saved model results.
CREATE OR REPLACE FUNCTION bot.delete_delivery_results() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM bot.interactions
 WHERE owner=NEW.owner AND (kind LIKE 'delivery_result:%' OR kind LIKE 'delivery_wire:%')
   AND COALESCE((content->>'generation')::bigint,0)<NEW.generation;
 RETURN NEW;
END;
$$;
