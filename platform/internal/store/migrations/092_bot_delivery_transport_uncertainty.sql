ALTER TABLE bot.delivery_intents
    ADD COLUMN last_uncertain_attempt bigint CHECK (last_uncertain_attempt > 0),
    ADD COLUMN last_uncertain_reason text,
    ADD COLUMN last_uncertain_recorded_at timestamptz,
    ADD COLUMN uncertain_resends smallint NOT NULL DEFAULT 0 CHECK (uncertain_resends BETWEEN 0 AND 3);

-- Historical rows keep unknown metadata absent until an actual recovery observation.
ALTER TABLE bot.delivery_intents ADD CONSTRAINT bot_delivery_uncertain_metadata_complete CHECK (
    (last_uncertain_attempt IS NULL AND last_uncertain_reason IS NULL AND last_uncertain_recorded_at IS NULL)
    OR (last_uncertain_attempt IS NOT NULL AND last_uncertain_reason IS NOT NULL AND last_uncertain_recorded_at IS NOT NULL)
);
