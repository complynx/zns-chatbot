-- +goose Up
ALTER TABLE shuttle_users ADD COLUMN ban_public_reason text NOT NULL DEFAULT ''
    CHECK (char_length(ban_public_reason) <= 1000);
ALTER TABLE shuttle_user_bans ADD COLUMN public_reason text NOT NULL DEFAULT ''
    CHECK (char_length(public_reason) <= 1000);

-- +goose Down
-- Keep published reasons in the audit when rolling application code back.
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_user_bans WHERE public_reason <> '') OR
       EXISTS (SELECT 1 FROM shuttle_users WHERE ban_public_reason <> '') THEN
        RAISE EXCEPTION 'public ban reasons must be preserved; use a reviewed forward migration';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_user_bans DROP COLUMN public_reason;
ALTER TABLE shuttle_users DROP COLUMN ban_public_reason;
