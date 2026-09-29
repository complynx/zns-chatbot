CREATE TABLE bot.delivery_intents (
    bot_id bigint NOT NULL CHECK (bot_id > 0),
    operation_key text NOT NULL CHECK (length(operation_key) BETWEEN 1 AND 200),
    effect_key text NOT NULL CHECK (length(effect_key) BETWEEN 1 AND 100),
    owner text NOT NULL,
    chat_id bigint NOT NULL CHECK (chat_id <> 0),
    reference jsonb NOT NULL,
    state text NOT NULL DEFAULT 'pending'
        CHECK (state IN ('pending','sending','sent','unknown','failed','cancelled','parked','paused')),
    phase text NOT NULL CHECK (phase IN ('send','edit','document')),
    target_message_id bigint NOT NULL DEFAULT 0 CHECK (target_message_id >= 0),
    attempt bigint NOT NULL DEFAULT 0 CHECK (attempt >= 0),
    attempted_at timestamptz,
    not_before timestamptz NOT NULL DEFAULT clock_timestamp(),
    message_id bigint NOT NULL DEFAULT 0 CHECK (message_id >= 0),
    reason text NOT NULL DEFAULT '',
    continuation_done boolean NOT NULL DEFAULT false,
    receipt jsonb NOT NULL DEFAULT '{}',
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (bot_id,operation_key,effect_key),
    CHECK (state <> 'sent' OR message_id > 0),
    CHECK (phase <> 'edit' OR target_message_id > 0)
);

-- Keep transport receipt evidence if the private source is deleted. Rendering
-- must cancel unsent effects whose source no longer exists; unknown stays fenced.
CREATE INDEX bot_delivery_receipts_pending
    ON bot.delivery_intents(bot_id,created_at)
    WHERE state='sent' AND NOT continuation_done;

CREATE SEQUENCE bot.delivery_effect_ids;

-- Private results belong to the conversation generation. The transport row
-- retains only references and receipt metadata when the user deletes history.
CREATE FUNCTION bot.delete_delivery_results() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 DELETE FROM bot.interactions
 WHERE owner=NEW.owner AND kind LIKE 'delivery_result:%'
   AND COALESCE((content->>'generation')::bigint,0)<NEW.generation;
 RETURN NEW;
END;
$$;
CREATE TRIGGER delete_bot_delivery_results
AFTER INSERT OR UPDATE OF generation ON core.conversation_history_generations
FOR EACH ROW EXECUTE FUNCTION bot.delete_delivery_results();