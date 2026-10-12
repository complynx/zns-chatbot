-- +goose Up
CREATE TABLE shuttle_users (
 id uuid PRIMARY KEY, bot_id bigint NOT NULL, tg_user_id bigint NOT NULL,
 profile jsonb NOT NULL DEFAULT '{}', zitadel_subject text NOT NULL DEFAULT '', zitadel_org text NOT NULL DEFAULT '', revoked boolean NOT NULL DEFAULT false,
 UNIQUE(bot_id,tg_user_id)
);
CREATE TABLE shuttle_conversations (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES shuttle_users(id), chat_id bigint NOT NULL,
 next_seq bigint NOT NULL DEFAULT 0, routed_seq bigint NOT NULL DEFAULT 0,
 active_task_id uuid, active_run_id text NOT NULL DEFAULT '', awaiting_question_id text NOT NULL DEFAULT '',
 UNIQUE(user_id,chat_id)
);
CREATE TABLE shuttle_tasks (
 id uuid PRIMARY KEY, user_id uuid NOT NULL REFERENCES shuttle_users(id), conversation_id uuid NOT NULL REFERENCES shuttle_conversations(id),
 seq bigint NOT NULL, request jsonb NOT NULL, state text NOT NULL DEFAULT 'accepted', route text NOT NULL DEFAULT '',
 target_task_id uuid NOT NULL DEFAULT '00000000-0000-0000-0000-000000000000', target_run_id text NOT NULL DEFAULT '', run_id text NOT NULL DEFAULT '', auth_session_id text NOT NULL DEFAULT '', question_id text NOT NULL DEFAULT '',
 snapshot jsonb NOT NULL DEFAULT '{}', version bigint NOT NULL DEFAULT 1, fence bigint NOT NULL DEFAULT 0,
 lease_owner text NOT NULL DEFAULT '', lease_until timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conversation_id,seq)
);
CREATE TABLE shuttle_inbox (
 bot_id bigint NOT NULL, update_id bigint NOT NULL, task_id uuid NOT NULL UNIQUE REFERENCES shuttle_tasks(id),
 payload jsonb NOT NULL, received_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(bot_id,update_id)
);
CREATE TABLE shuttle_outbox (
 id uuid PRIMARY KEY, task_id uuid NOT NULL REFERENCES shuttle_tasks(id), dedup_key text NOT NULL UNIQUE, chunk_seq integer NOT NULL,
 payload jsonb NOT NULL, state text NOT NULL DEFAULT 'pending', attempt_id uuid, receipt jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(task_id,chunk_seq)
);
CREATE TABLE shuttle_identity_audit (id bigserial PRIMARY KEY,user_id uuid NOT NULL REFERENCES shuttle_users(id),actor text NOT NULL,revoked boolean NOT NULL,changed_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE shuttle_settings (id boolean PRIMARY KEY DEFAULT true CHECK(id), version bigint NOT NULL, document jsonb NOT NULL);
CREATE TABLE shuttle_settings_audit (id bigserial PRIMARY KEY, actor text NOT NULL, version bigint NOT NULL, old_document jsonb NOT NULL, new_document jsonb NOT NULL, changed_at timestamptz NOT NULL DEFAULT now());
INSERT INTO shuttle_settings(id,version,document) VALUES(true,1,'{"default_language":"en","max_parallel_tasks":8,"question_timeout_seconds":3600,"auth_cache_ttl_seconds":0}');
-- +goose Down
DROP TABLE shuttle_identity_audit, shuttle_settings_audit, shuttle_settings, shuttle_outbox, shuttle_inbox, shuttle_tasks, shuttle_conversations, shuttle_users;
