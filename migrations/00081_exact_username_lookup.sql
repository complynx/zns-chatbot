-- +goose Up
-- Nonunique: duplicate/stale usernames must remain detectable. The final ID
-- supports the resolver's deterministic two-row ambiguity probe.
CREATE INDEX shuttle_users_username_idx
    ON shuttle_users (bot_id, zitadel_org, lower(profile->>'username'), id);

-- +goose Down
DROP INDEX shuttle_users_username_idx;
