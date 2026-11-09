-- +goose Up
CREATE TABLE shuttle_telegram_availability (
    bot_id bigint NOT NULL CHECK (bot_id > 0),
    telegram_user_id bigint NOT NULL CHECK (telegram_user_id > 0),
    revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    reason text NOT NULL DEFAULT '' CHECK (reason IN ('', 'user_deactivated', 'bot_blocked')),
    updated_at timestamptz NOT NULL DEFAULT statement_timestamp(),
    PRIMARY KEY (bot_id, telegram_user_id)
);

-- +goose Down
-- Do not silently resume unavailable recipients during a schema rollback.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_telegram_availability WHERE reason <> '') THEN
        RAISE EXCEPTION 'Telegram suspensions must be preserved; use a reviewed forward migration';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_telegram_availability;
