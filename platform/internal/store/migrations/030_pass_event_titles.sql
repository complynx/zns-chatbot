ALTER TABLE core.pass_events ADD COLUMN titles jsonb NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE core.pass_events ADD COLUMN display_order integer NOT NULL DEFAULT 0;
ALTER TABLE core.pass_events ADD CONSTRAINT pass_event_titles_object CHECK(jsonb_typeof(titles)='object');
