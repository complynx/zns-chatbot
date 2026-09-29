-- Additive food compatibility storage; existing orders and pass rows are unchanged.
ALTER TABLE core.legacy_user_deferred_domains DROP CONSTRAINT legacy_user_deferred_domains_domain_check;
ALTER TABLE core.legacy_user_deferred_domains ADD CHECK(domain IN ('passes','food'));
CREATE TABLE core.food_events (
 event_id text PRIMARY KEY REFERENCES core.pass_events(id),
 bot_id bigint NOT NULL CHECK(bot_id>0),
 menu jsonb NOT NULL, menu_sha256 text NOT NULL CHECK(menu_sha256 ~ '^[0-9a-f]{64}$'),
 meal_prices jsonb NOT NULL, activity_prices jsonb NOT NULL,
 deadline timestamptz NOT NULL, cacao_capacity integer NOT NULL CHECK(cacao_capacity>=0),
 first_before interval NOT NULL CHECK(first_before>interval '0'),
 last_before interval NOT NULL CHECK(last_before>interval '0'),
 notify_after interval NOT NULL CHECK(notify_after>=interval '0'),
 CHECK(first_before>last_before)
);
CREATE TABLE core.food_admins (
 event_id text NOT NULL REFERENCES core.food_events(event_id),
 owner text NOT NULL REFERENCES core.users(id),
 can_export boolean NOT NULL DEFAULT false, can_review boolean NOT NULL DEFAULT false,
 can_assign boolean NOT NULL DEFAULT false, instructions jsonb NOT NULL DEFAULT '{}',
 PRIMARY KEY(event_id,owner)
);
CREATE TABLE core.legacy_food_import_references (
 source_key text PRIMARY KEY CHECK(source_key ~ '^[0-9a-f]{64}$'),
 bot_id bigint NOT NULL CHECK(bot_id>0), event_id text NOT NULL REFERENCES core.food_events(event_id),
 source_kind text NOT NULL CHECK(source_kind IN ('configuration','food','proof','pass_marker')),
 source_record_sha256 text NOT NULL CHECK(source_record_sha256 ~ '^[0-9a-f]{64}$'),
 target_id text NOT NULL, source_record jsonb NOT NULL,
 UNIQUE(bot_id,source_kind,target_id)
);
CREATE TABLE core.food_orders (
 id text PRIMARY KEY, event_id text NOT NULL REFERENCES core.food_events(event_id),
 owner text NOT NULL REFERENCES core.users(id), version bigint NOT NULL CHECK(version>0),
 meals jsonb NOT NULL DEFAULT '{}', meal_total bigint NOT NULL DEFAULT 0 CHECK(meal_total>=0),
 complete boolean NOT NULL DEFAULT false, activities jsonb NOT NULL DEFAULT '{}',
 activity_total bigint NOT NULL DEFAULT 0 CHECK(activity_total>=0),
 payment_admin text REFERENCES core.users(id), created_at timestamptz NOT NULL,
 last_updated timestamptz, UNIQUE(event_id,owner)
);
-- Each replacement is a new generation. Imported receipt generation zero retains
-- its raw source attribution; later replacements never overwrite its history.
CREATE TABLE core.food_payments (
 order_id text NOT NULL REFERENCES core.food_orders(id),
 kind text NOT NULL CHECK(kind IN ('meals','activities')),
 generation bigint NOT NULL CHECK(generation>=0),
 status text NOT NULL CHECK(status IN ('pending','proof_submitted','paid','rejected')),
 proof_id text REFERENCES core.order_proofs(id), proof_source text NOT NULL DEFAULT '',
 receiver text REFERENCES core.users(id), received_at timestamptz,
 confirmed_by text REFERENCES core.users(id), confirmed_at timestamptz,
 rejected_by text REFERENCES core.users(id), rejected_at timestamptz,
 legacy_source_key text REFERENCES core.legacy_food_import_references(source_key),
 PRIMARY KEY(order_id,kind,generation),
 CHECK(legacy_source_key IS NOT NULL OR status='pending' OR (proof_id IS NOT NULL AND received_at IS NOT NULL))
);
CREATE TABLE core.food_operations (
 event_id text NOT NULL REFERENCES core.food_events(event_id), actor text NOT NULL REFERENCES core.users(id),
 key_hash text NOT NULL, request_hash text NOT NULL, result jsonb NOT NULL,
 PRIMARY KEY(event_id,actor,key_hash)
);
CREATE TABLE core.food_notifications (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 event_id text NOT NULL REFERENCES core.food_events(event_id), owner text NOT NULL REFERENCES core.users(id),
 kind text NOT NULL, subject text NOT NULL DEFAULT '',
 payload jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz, legacy_source_key text REFERENCES core.legacy_food_import_references(source_key),
 imported_sent boolean NOT NULL DEFAULT false,
 UNIQUE(event_id,owner,kind,subject),
 CHECK(NOT imported_sent OR legacy_source_key IS NOT NULL)
);
ALTER TABLE bot.media_intake ADD COLUMN food_command jsonb;
CREATE TABLE bot.food_buttons (
 owner text NOT NULL REFERENCES core.users(id), token text NOT NULL, command jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours',
 consumed_at timestamptz, PRIMARY KEY(owner,token)
);
CREATE TABLE bot.food_pending (
 owner text PRIMARY KEY REFERENCES core.users(id), command jsonb NOT NULL, update_id bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours'
);
CREATE TABLE bot.food_legacy_callbacks (
 owner text NOT NULL REFERENCES core.users(id), callback_hash text NOT NULL, binding jsonb NOT NULL,
 PRIMARY KEY(owner,callback_hash)
);
CREATE TABLE core.food_legacy_menu_bindings (
 actor text NOT NULL REFERENCES core.users(id), event_id text NOT NULL REFERENCES core.food_events(event_id),
 key_hash text NOT NULL, request_hash text NOT NULL, command jsonb NOT NULL,
 PRIMARY KEY(actor,event_id,key_hash)
);
