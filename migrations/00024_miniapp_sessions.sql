-- +goose Up
CREATE TABLE shuttle_miniapp_sessions (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL CHECK(bot_id>0),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 proof_hash bytea NOT NULL CHECK(octet_length(proof_hash)=32),
 nonce_hash bytea NOT NULL CHECK(octet_length(nonce_hash)=32),
 key_generation bytea NOT NULL CHECK(octet_length(key_generation)=32),
 bearer_hash bytea CHECK(octet_length(bearer_hash)=32),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','active','revoked')),
 subject text NOT NULL DEFAULT '', organization text NOT NULL DEFAULT '',
 auth_date timestamptz NOT NULL, pending_until timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(bot_id,proof_hash), UNIQUE(bot_id,bearer_hash),
 CHECK(state<>'active' OR (bearer_hash IS NOT NULL AND subject<>'' AND organization<>''))
);
CREATE INDEX shuttle_miniapp_session_cleanup ON shuttle_miniapp_sessions(bot_id,auth_date);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_miniapp_sessions) THEN
  RAISE EXCEPTION 'Mini App sessions and proof consumption must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_miniapp_sessions;
