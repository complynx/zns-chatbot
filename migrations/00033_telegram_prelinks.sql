-- +goose Up
-- Abort on duplicate existing bindings; never merge accounts during migration.
CREATE UNIQUE INDEX shuttle_user_bound_identity
 ON shuttle_users(bot_id,zitadel_org,zitadel_subject) WHERE zitadel_subject<>'';

CREATE TABLE shuttle_telegram_prelinks (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL CHECK(bot_id>0),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 provider_issuer text NOT NULL CHECK(length(provider_issuer) BETWEEN 1 AND 2048),
 assertion_jti text NOT NULL CHECK(length(assertion_jti) BETWEEN 1 AND 200),
 assertion_hash bytea NOT NULL CHECK(octet_length(assertion_hash)=32),
 organization text NOT NULL CHECK(length(organization) BETWEEN 1 AND 200),
 provider_id text NOT NULL CHECK(length(provider_id) BETWEEN 1 AND 200),
 external_subject text NOT NULL CHECK(length(external_subject) BETWEEN 1 AND 200),
 expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','completed')),
 subject text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 completed_at timestamptz,
 UNIQUE(provider_issuer,assertion_jti),
 CHECK((state='pending' AND subject='' AND completed_at IS NULL)
    OR (state='completed' AND length(subject) BETWEEN 1 AND 200
        AND completed_at IS NOT NULL AND completed_at<=expires_at))
);

-- +goose Down
-- Keep accepted intent/proof consumption facts unless exported explicitly.
LOCK TABLE shuttle_telegram_prelinks IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_telegram_prelinks) THEN
  RAISE EXCEPTION 'Telegram prelink receipts must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_telegram_prelinks;
DROP INDEX shuttle_user_bound_identity;
