ALTER TABLE core.admin_messages ADD COLUMN command text;
ALTER TABLE core.admin_message_deliveries ADD COLUMN content jsonb;
CREATE TABLE core.admin_message_inputs (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 actor text NOT NULL REFERENCES core.users(id),
 key text NOT NULL,
 chat_id bigint NOT NULL,
 command text NOT NULL,
 prompt_id bigint NOT NULL DEFAULT 0,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','attached','cancelled','expired')),
 message_id bigint REFERENCES core.admin_messages(id),
 attachment_key text,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
 UNIQUE(actor,key)
);
CREATE INDEX admin_message_inputs_pending ON core.admin_message_inputs(actor,chat_id,expires_at) WHERE state='pending';
CREATE TABLE core.admin_broadcast_profiles (
 owner text PRIMARY KEY REFERENCES core.users(id),
 source_key text,
 source_hash text,
 fields jsonb NOT NULL CHECK(jsonb_typeof(fields)='object'),
 overrides jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(overrides)='object')
);
CREATE TABLE core.admin_message_recipients (
 message_id bigint NOT NULL REFERENCES core.admin_messages(id),
 position bigint NOT NULL,
 destination jsonb NOT NULL,
 content jsonb NOT NULL,
 failure text NOT NULL DEFAULT '',
 PRIMARY KEY(message_id,position),
 UNIQUE(message_id,destination)
);
INSERT INTO core.admin_message_recipients(message_id,position,destination,content)
 SELECT m.id,r.position,r.destination,m.request->'content' FROM core.admin_messages m
 CROSS JOIN LATERAL jsonb_array_elements(m.request->'destinations') WITH ORDINALITY AS r(destination,position);
ALTER TABLE core.admin_messages DROP CONSTRAINT admin_messages_state_check;
ALTER TABLE core.admin_messages ADD CHECK(state IN ('preparing','draft','queued','cancelled'));
ALTER TABLE core.admin_message_recipients ADD COLUMN profile jsonb;
ALTER TABLE core.admin_message_recipients ADD COLUMN profile_owner text REFERENCES core.users(id);
ALTER TABLE core.admin_message_recipients ADD COLUMN render_state text NOT NULL DEFAULT 'done' CHECK(render_state IN ('pending','rendering','done'));
ALTER TABLE core.admin_message_recipients ADD COLUMN render_attempt bigint NOT NULL DEFAULT 0;
ALTER TABLE core.admin_message_recipients ADD COLUMN render_available_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE core.admin_message_inputs ADD COLUMN notice_attempt bigint NOT NULL DEFAULT 0;
ALTER TABLE core.admin_message_inputs ADD COLUMN notice_available_at timestamptz NOT NULL DEFAULT clock_timestamp();
ALTER TABLE core.admin_message_inputs ADD COLUMN notice_done boolean NOT NULL DEFAULT false;
CREATE TABLE core.admin_message_sources (
 actor text NOT NULL REFERENCES core.users(id),
 key text NOT NULL,
 chat_id bigint NOT NULL,
 message_id bigint NOT NULL,
 text_html text NOT NULL,
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '10 minutes',
 PRIMARY KEY(actor,key)
);
-- Existing native identities have no legacy field-presence document. Retain only
-- guaranteed identity and metadata with explicit evidence of a trusted update.
INSERT INTO core.admin_broadcast_profiles(owner,fields,overrides)
 SELECT u.id,jsonb_build_object('user_id',u.telegram_id),
 CASE WHEN u.telegram_metadata_update>=0 THEN jsonb_build_object('username',u.username,'first_name',u.first_name,'last_name',u.last_name,'print_name',u.print_name) ELSE '{}'::jsonb END
 FROM core.users u WHERE NOT EXISTS(SELECT 1 FROM core.legacy_user_references l WHERE l.owner=u.id)
 ON CONFLICT DO NOTHING;
