-- Decisions survive retries and process restarts, including denied updates.
CREATE TABLE bot.agent_quota (
 owner text NOT NULL REFERENCES core.users(id),
 update_id bigint NOT NULL,
 allowed boolean NOT NULL,
 remaining integer NOT NULL CHECK (remaining >= 0),
 reserved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (owner, update_id)
);
CREATE INDEX agent_quota_window ON bot.agent_quota(owner, reserved_at) WHERE allowed;
