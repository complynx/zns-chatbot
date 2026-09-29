-- Add source event configuration without changing existing catalog rows.
ALTER TABLE core.pass_events
 ADD COLUMN short_titles jsonb NOT NULL DEFAULT '{}'::jsonb CHECK(jsonb_typeof(short_titles)='object'),
 ADD COLUMN country_emoji text NOT NULL DEFAULT '',
 ADD COLUMN thread_channel text NOT NULL DEFAULT '',
 ADD COLUMN thread_id bigint,
 ADD COLUMN thread_locale text NOT NULL DEFAULT 'ru',
 ADD COLUMN default_price integer,
 ADD COLUMN amount_cap_per_role integer NOT NULL DEFAULT 80,
 ADD COLUMN open_ended boolean NOT NULL DEFAULT false;

-- A registration generation is announced once; retries never create a new item.
CREATE TABLE core.pass_registration_announcements (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_id text NOT NULL, owner text NOT NULL, created_at timestamptz NOT NULL,
 channel text NOT NULL, thread_id bigint, locale text NOT NULL,
 name text NOT NULL, role text NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','sending','sent','failed','unknown','suppressed')),
 available_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 attempts integer NOT NULL DEFAULT 0, message_id bigint NOT NULL DEFAULT 0,
 failure text NOT NULL DEFAULT '',
 UNIQUE(event_id,owner,created_at),
 FOREIGN KEY(event_id,owner) REFERENCES core.pass_bookings(event_id,owner)
);

CREATE TABLE core.legacy_pass_announcement_metadata (
 source_key text PRIMARY KEY REFERENCES core.legacy_pass_import_references(source_key),
 event_id text NOT NULL, owner text NOT NULL, created_at timestamptz NOT NULL,
 marker_present boolean NOT NULL, source_marker jsonb,
 policy text NOT NULL CHECK(policy IN ('source_marker','suppress_historical','preserve_source_eligibility')),
 FOREIGN KEY(event_id,owner) REFERENCES core.pass_bookings(event_id,owner)
);

-- Pre-058 imports carry no announcement policy: preserve history without sends.
INSERT INTO core.pass_registration_announcements(event_id,owner,created_at,channel,locale,name,role,state,failure)
SELECT b.event_id,b.owner,b.created_at,'','ru',u.name,b.role,'suppressed','legacy_before_058'
FROM core.pass_bookings b JOIN core.users u ON u.id=b.owner
WHERE EXISTS(SELECT 1 FROM core.legacy_pass_import_references l WHERE l.event_id=b.event_id AND l.owner=b.owner AND l.source_kind IN ('booking','embedded'));
