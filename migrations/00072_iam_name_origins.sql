-- +goose Up
CREATE TABLE shuttle_iam_name_origins (
 user_id uuid PRIMARY KEY REFERENCES shuttle_users(id),
 origin text NOT NULL CHECK(origin IN ('unknown','technical','existing')),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 checked_at timestamptz
);
-- Older provisioning copied unconfirmed Telegram names into IAM. Preserve
-- uncertainty instead of silently treating those names as user confirmation.
INSERT INTO shuttle_iam_name_origins(user_id,origin) SELECT id,'unknown' FROM shuttle_users;

-- +goose Down
LOCK TABLE shuttle_iam_name_origins IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_iam_name_origins WHERE origin<>'unknown') THEN
  RAISE EXCEPTION 'IAM name provenance must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_iam_name_origins;
