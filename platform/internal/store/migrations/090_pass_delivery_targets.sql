-- Resolve the actual pass-card payload by its saved successful Telegram target.
-- Owner/chat and the pass family prevent unrelated receipt adoption.
CREATE INDEX bot_pass_delivery_targets
    ON bot.delivery_intents(bot_id,owner,chat_id,message_id,attempted_at DESC NULLS LAST,created_at DESC,operation_key DESC,effect_key DESC)
    WHERE state='sent' AND reference->>'family'='passes';
