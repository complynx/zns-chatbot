-- +goose Up
CREATE TABLE shuttle_pass_tiers (
 pass_id uuid PRIMARY KEY REFERENCES shuttle_passes(id),
 tier_id text NOT NULL CHECK(tier_id ~ '^[a-z0-9][a-z0-9_-]{0,63}$'),
 tier_number integer NOT NULL CHECK(tier_number BETWEEN 1 AND 100)
);
-- +goose Down
-- Export tier history first. Refuse rollback while it is retained.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_pass_tiers) THEN
  RAISE EXCEPTION 'pass tier history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_pass_tiers;
