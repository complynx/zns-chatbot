-- +goose Up
ALTER TABLE shuttle_events ADD COLUMN slug text NOT NULL DEFAULT ''
    CHECK (slug = '' OR (slug ~ '^[a-z0-9][a-z0-9_-]{0,63}$'
        AND slug !~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'));
CREATE UNIQUE INDEX shuttle_events_slug_unique ON shuttle_events(bot_id,organization_id,slug) WHERE slug <> '';

-- +goose Down
LOCK TABLE shuttle_events IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_events WHERE slug <> '') THEN
        RAISE EXCEPTION 'event slugs must be exported and cleared before downgrade';
    END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_events DROP COLUMN slug;
