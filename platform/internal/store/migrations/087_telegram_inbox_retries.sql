-- Bounded inbox retries. Additive only: existing rows stay pending with zero
-- recorded failures, due immediately; payload and update_id are never rewritten.
-- failures counts only conclusively recorded handler failures. next_attempt_at
-- is a scheduling lease/cooldown and never poison evidence. Quarantine is
-- terminal, retains the original payload and stores only a fixed diagnostic.
-- chat_key mirrors parseUpdate: a present callback_query object decides the
-- key, otherwise message; only private-range positive integers produce a key.
-- Nested CASE guards type and range before every cast (AND order is undefined),
-- so malformed, fractional or huge values yield NULL instead of failing. NULL
-- keys never compare equal and therefore never serialize unrelated rows.
ALTER TABLE bot.telegram_inbox
 ADD COLUMN state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending','quarantined')),
 ADD COLUMN failures integer NOT NULL DEFAULT 0 CHECK (failures >= 0),
 ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT '-infinity',
 ADD COLUMN failure text NOT NULL DEFAULT ''
  CHECK (failure IN ('','handler','malformed','update_mismatch')),
 ADD COLUMN quarantined_at timestamptz,
 ADD COLUMN chat_key bigint GENERATED ALWAYS AS (
  CASE
   WHEN jsonb_typeof(payload->'callback_query')='object' THEN
    CASE WHEN jsonb_typeof(payload#>'{callback_query,message,chat,id}')='number' THEN
     CASE WHEN (payload#>'{callback_query,message,chat,id}')::numeric > 0
       AND (payload#>'{callback_query,message,chat,id}')::numeric < 4503599627370496
       AND (payload#>'{callback_query,message,chat,id}')::numeric
        = trunc((payload#>'{callback_query,message,chat,id}')::numeric)
     THEN (payload#>'{callback_query,message,chat,id}')::numeric::bigint END
    END
   WHEN jsonb_typeof(payload->'message')='object' THEN
    CASE WHEN jsonb_typeof(payload#>'{message,chat,id}')='number' THEN
     CASE WHEN (payload#>'{message,chat,id}')::numeric > 0
       AND (payload#>'{message,chat,id}')::numeric < 4503599627370496
       AND (payload#>'{message,chat,id}')::numeric = trunc((payload#>'{message,chat,id}')::numeric)
     THEN (payload#>'{message,chat,id}')::numeric::bigint END
    END
  END) STORED,
 ADD CONSTRAINT telegram_inbox_quarantine_time CHECK ((state='quarantined') = (quarantined_at IS NOT NULL));
CREATE INDEX telegram_inbox_pending_chat ON bot.telegram_inbox(chat_key,update_id) WHERE state='pending';
-- Runtime roles that already read/insert/delete inbox rows now record retry state.
DO $$
DECLARE runtime_role text;
BEGIN
 FOR runtime_role IN SELECT rolname FROM pg_roles WHERE rolname IN ('zns_app','zns_bot','zns_runtime') LOOP
  EXECUTE format('GRANT UPDATE ON bot.telegram_inbox TO %I',runtime_role);
 END LOOP;
END $$;
