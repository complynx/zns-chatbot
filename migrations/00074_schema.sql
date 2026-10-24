-- +goose Up
-- Fresh databases receive the preserved version-73 schema, then the Latin pair.
-- Completed version-73 databases receive only the compatible Latin extension.
-- +goose StatementBegin
DO $shuttle_baseline74$
DECLARE applied bigint;
BEGIN
 SELECT max(version_id) INTO applied FROM (
  SELECT DISTINCT ON (version_id) version_id,is_applied
  FROM goose_db_version ORDER BY version_id,id DESC
 ) history WHERE is_applied AND version_id>0;
 IF applied IS NULL THEN
-- BEGIN PRESERVED VERSION 73 DDL
CREATE FUNCTION shuttle_guard_assignment_namespace() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(format('registration:%s:%s:%s:%s',
  NEW.bot_id,NEW.organization_id,NEW.actor,NEW.operation_id),0));
 IF TG_TABLE_NAME='shuttle_assignment_aliases' THEN
  IF EXISTS(SELECT 1 FROM shuttle_assignment_preparations p WHERE p.bot_id=NEW.bot_id
   AND p.organization_id=NEW.organization_id AND p.actor=NEW.actor AND p.operation_id=NEW.operation_id)
   OR EXISTS(SELECT 1 FROM shuttle_registration_operations r WHERE r.bot_id=NEW.bot_id
   AND r.organization_id=NEW.organization_id AND r.actor=NEW.actor AND r.operation_id=NEW.operation_id) THEN
   RAISE EXCEPTION 'assignment alias key is already occupied' USING ERRCODE='23505';
  END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM shuttle_assignment_aliases a WHERE a.bot_id=NEW.bot_id
   AND a.organization_id=NEW.organization_id AND a.actor=NEW.actor AND a.operation_id=NEW.operation_id) THEN
   RAISE EXCEPTION 'assignment operation is already an alias' USING ERRCODE='23505';
  END IF;
  IF TG_TABLE_NAME='shuttle_assignment_preparations' THEN
   IF EXISTS(SELECT 1 FROM shuttle_registration_operations r WHERE r.bot_id=NEW.bot_id
    AND r.organization_id=NEW.organization_id AND r.actor=NEW.actor AND r.operation_id=NEW.operation_id
    AND r.request_hash<>NEW.request_hash) THEN
    RAISE EXCEPTION 'assignment key has a different registration receipt' USING ERRCODE='23505';
   END IF;
  ELSE
   IF EXISTS(SELECT 1 FROM shuttle_assignment_preparations p WHERE p.bot_id=NEW.bot_id
    AND p.organization_id=NEW.organization_id AND p.actor=NEW.actor AND p.operation_id=NEW.operation_id
    AND p.request_hash<>NEW.request_hash) THEN
    RAISE EXCEPTION 'registration key has a different assignment preparation' USING ERRCODE='23505';
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END;
$$;

CREATE FUNCTION shuttle_guard_pair_assignment_namespace() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(format('pair:%s:%s:%s:%s',
  NEW.bot_id,NEW.organization_id,NEW.actor,NEW.operation_id),0));
 IF TG_TABLE_NAME='shuttle_pair_assignment_aliases' THEN
  IF EXISTS(SELECT 1 FROM shuttle_pair_preparations p WHERE p.bot_id=NEW.bot_id
   AND p.organization_id=NEW.organization_id AND p.actor=NEW.actor AND p.operation_id=NEW.operation_id)
   OR EXISTS(SELECT 1 FROM shuttle_pair_operations r WHERE r.bot_id=NEW.bot_id
   AND r.organization_id=NEW.organization_id AND r.actor=NEW.actor AND r.operation_id=NEW.operation_id) THEN
   RAISE EXCEPTION 'assignment alias key is already occupied' USING ERRCODE='23505';
  END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM shuttle_pair_assignment_aliases a WHERE a.bot_id=NEW.bot_id
   AND a.organization_id=NEW.organization_id AND a.actor=NEW.actor AND a.operation_id=NEW.operation_id) THEN
   RAISE EXCEPTION 'assignment operation is already an alias' USING ERRCODE='23505';
  END IF;
  IF TG_TABLE_NAME='shuttle_pair_preparations' THEN
   IF EXISTS(SELECT 1 FROM shuttle_pair_operations r WHERE r.bot_id=NEW.bot_id
    AND r.organization_id=NEW.organization_id AND r.actor=NEW.actor AND r.operation_id=NEW.operation_id
    AND (r.kind<>'assign' OR r.request_hash<>NEW.request_hash)) THEN
    RAISE EXCEPTION 'assignment key has a different pair receipt' USING ERRCODE='23505';
   END IF;
  ELSE
   IF EXISTS(SELECT 1 FROM shuttle_pair_preparations p WHERE p.bot_id=NEW.bot_id
    AND p.organization_id=NEW.organization_id AND p.actor=NEW.actor AND p.operation_id=NEW.operation_id
    AND (NEW.kind<>'assign' OR p.request_hash<>NEW.request_hash)) THEN
    RAISE EXCEPTION 'pair receipt key has a different assignment preparation' USING ERRCODE='23505';
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END;
$$;

CREATE TABLE shuttle_active_pair_members (
    registration_id uuid NOT NULL,
    pair_id uuid NOT NULL,
    event_id uuid NOT NULL,
    role text NOT NULL,
    CONSTRAINT shuttle_active_pair_members_role_check CHECK ((role = ANY (ARRAY['leader'::text, 'follower'::text])))
);

CREATE TABLE shuttle_assignment_aliases (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    canonical_operation_id text NOT NULL,
    CONSTRAINT shuttle_assignment_aliases_check CHECK ((operation_id <> canonical_operation_id)),
    CONSTRAINT shuttle_assignment_aliases_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128)))
);

CREATE TABLE shuttle_assignment_preparations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL
);

CREATE TABLE shuttle_authority_operations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_authority_preparations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL
);

CREATE TABLE shuttle_avatar_deliveries (
    id uuid NOT NULL,
    artifact_id uuid NOT NULL,
    user_id uuid NOT NULL,
    telegram_user_id bigint NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_avatar_deliveries_check CHECK (((state <> 'send_inflight'::text) OR (attempt_id IS NOT NULL))),
    CONSTRAINT shuttle_avatar_deliveries_receipt_check CHECK (((jsonb_typeof(receipt) = 'object'::text) AND (octet_length((receipt)::text) <= 4096))),
    CONSTRAINT shuttle_avatar_deliveries_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_avatar_deliveries_telegram_user_id_check CHECK ((telegram_user_id > 0)),
    CONSTRAINT shuttle_avatar_deliveries_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_avatar_delivery_resolutions (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id uuid NOT NULL,
    proposal_id uuid NOT NULL,
    delivery_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL,
    result jsonb,
    CONSTRAINT shuttle_avatar_delivery_resolutions_document_check CHECK (((jsonb_typeof(document) = 'object'::text) AND (octet_length((document)::text) <= 16384))),
    CONSTRAINT shuttle_avatar_delivery_resolutions_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_avatar_delivery_resolutions_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 16384)))
);

CREATE TABLE shuttle_avatar_draft_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL,
    request_hash bytea NOT NULL,
    draft_id uuid NOT NULL,
    result jsonb NOT NULL,
    native boolean NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_avatar_draft_operations_kind_check CHECK ((kind = ANY (ARRAY['create_upload'::text, 'create_file'::text, 'create_history'::text, 'save_transform'::text]))),
    CONSTRAINT shuttle_avatar_draft_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_avatar_draft_operations_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 4096)))
);

CREATE TABLE shuttle_avatar_drafts (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    source_file_id uuid,
    original bytea,
    source_sha256 bytea NOT NULL,
    mime_type text NOT NULL,
    frame_sha256 bytea NOT NULL,
    transform jsonb NOT NULL,
    version bigint NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    updated_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    source_size_bytes bigint NOT NULL,
    history_record_id uuid,
    history_archive_id uuid,
    original_expired_at timestamp with time zone,
    CONSTRAINT shuttle_avatar_drafts_frame_sha256_check CHECK ((octet_length(frame_sha256) = 32)),
    CONSTRAINT shuttle_avatar_drafts_mime_type_check CHECK ((mime_type = ANY (ARRAY['image/jpeg'::text, 'image/png'::text]))),
    CONSTRAINT shuttle_avatar_drafts_original_check CHECK (((octet_length(original) > 0) AND (octet_length(original) <= 20971520))),
    CONSTRAINT shuttle_avatar_drafts_source_sha256_check CHECK ((octet_length(source_sha256) = 32)),
    CONSTRAINT shuttle_avatar_drafts_transform_check CHECK (((jsonb_typeof(transform) = 'object'::text) AND (octet_length((transform)::text) <= 1024))),
    CONSTRAINT shuttle_avatar_drafts_version_check CHECK ((version > 0)),
    CONSTRAINT shuttle_avatar_source_shape CHECK ((((source_file_id IS NOT NULL) AND (history_record_id IS NULL) AND (history_archive_id IS NULL) AND (original IS NULL) AND (original_expired_at IS NULL)) OR ((source_file_id IS NULL) AND (history_record_id IS NOT NULL) AND (history_archive_id IS NOT NULL) AND (original IS NULL) AND (original_expired_at IS NULL)) OR ((source_file_id IS NULL) AND (history_record_id IS NULL) AND (history_archive_id IS NULL) AND (((original IS NOT NULL) AND (octet_length(original) = source_size_bytes) AND (original_expired_at IS NULL)) OR ((original IS NULL) AND (original_expired_at IS NOT NULL)))))),
    CONSTRAINT shuttle_avatar_source_size CHECK (((source_size_bytes > 0) AND (source_size_bytes <= 20971520)))
);

CREATE TABLE shuttle_avatar_resolution_aliases (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id uuid NOT NULL,
    canonical_operation_id uuid NOT NULL,
    CONSTRAINT shuttle_avatar_resolution_aliases_check CHECK ((operation_id <> canonical_operation_id))
);

CREATE TABLE shuttle_avatars (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    task_id uuid,
    delivery_id uuid,
    source_file_id uuid,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    receipt jsonb NOT NULL,
    jpeg bytea NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    draft_id uuid,
    draft_version bigint,
    native boolean DEFAULT false NOT NULL,
    CONSTRAINT shuttle_avatars_jpeg_check CHECK (((octet_length(jpeg) > 0) AND (octet_length(jpeg) <= 4194304))),
    CONSTRAINT shuttle_avatars_origin CHECK ((((task_id IS NOT NULL) AND (delivery_id IS NOT NULL) AND (source_file_id IS NOT NULL) AND (draft_id IS NULL) AND (draft_version IS NULL) AND (NOT native)) OR ((task_id IS NULL) AND (delivery_id IS NULL) AND (source_file_id IS NULL) AND (draft_id IS NOT NULL) AND (draft_version IS NOT NULL) AND (draft_version > 0)))),
    CONSTRAINT shuttle_avatars_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_conversations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    chat_id bigint NOT NULL,
    next_seq bigint DEFAULT 0 NOT NULL,
    routed_seq bigint DEFAULT 0 NOT NULL,
    active_task_id uuid,
    active_run_id text DEFAULT ''::text NOT NULL,
    awaiting_question_id text DEFAULT ''::text NOT NULL
);

CREATE TABLE shuttle_deployment (
    id boolean DEFAULT true NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    issuer text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT shuttle_deployment_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_deployment_id_check CHECK (id),
    CONSTRAINT shuttle_deployment_issuer_check CHECK ((issuer <> ''::text)),
    CONSTRAINT shuttle_deployment_organization_id_check CHECK ((organization_id <> ''::text))
);

CREATE TABLE shuttle_event_authorities (
    event_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT shuttle_event_authorities_role_check CHECK ((role = ANY (ARRAY['none'::text, 'ambassador'::text, 'administrator'::text]))),
    CONSTRAINT shuttle_event_authorities_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_event_change_proposals (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL,
    result jsonb,
    CONSTRAINT shuttle_event_change_proposals_actor_check CHECK ((actor <> ''::text)),
    CONSTRAINT shuttle_event_change_proposals_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_event_change_proposals_document_check CHECK ((jsonb_typeof(document) = 'object'::text)),
    CONSTRAINT shuttle_event_change_proposals_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128))),
    CONSTRAINT shuttle_event_change_proposals_organization_id_check CHECK ((organization_id <> ''::text)),
    CONSTRAINT shuttle_event_change_proposals_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_event_change_proposals_result_check CHECK ((jsonb_typeof(result) = 'object'::text))
);

CREATE TABLE shuttle_event_context_operations (
    user_id uuid NOT NULL,
    operation_id text NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_event_deposit_policy (
    event_id uuid NOT NULL,
    document jsonb
);

CREATE TABLE shuttle_event_operations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_event_registration_rules (
    event_id uuid NOT NULL,
    document jsonb,
    CONSTRAINT shuttle_event_registration_rules_document_check CHECK (((document IS NULL) OR (jsonb_typeof(document) = 'object'::text)))
);

CREATE TABLE shuttle_events (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    name text NOT NULL,
    timezone text NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    ends_at timestamp with time zone NOT NULL,
    modules text[] DEFAULT '{}'::text[] NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    CONSTRAINT shuttle_events_check CHECK ((ends_at > starts_at)),
    CONSTRAINT shuttle_events_modules_check CHECK ((cardinality(modules) <= 100)),
    CONSTRAINT shuttle_events_name_check CHECK (((char_length(name) >= 1) AND (char_length(name) <= 256))),
    CONSTRAINT shuttle_events_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_files (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    task_id uuid NOT NULL,
    telegram_id text NOT NULL,
    unique_id text NOT NULL,
    kind text NOT NULL,
    mime_type text NOT NULL,
    filename text NOT NULL,
    size_bytes bigint NOT NULL,
    message_id bigint NOT NULL,
    sent_at timestamp with time zone NOT NULL,
    accepted_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_files_message_id_check CHECK ((message_id > 0)),
    CONSTRAINT shuttle_files_size_bytes_check CHECK ((size_bytes >= 0))
);

CREATE TABLE shuttle_history_account_receipts (
    record_id uuid NOT NULL,
    user_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    receipt jsonb NOT NULL,
    CONSTRAINT shuttle_history_account_receipts_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_iam_name_origins (
    user_id uuid NOT NULL,
    origin text NOT NULL,
    recorded_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    checked_at timestamp with time zone,
    CONSTRAINT shuttle_iam_name_origins_origin_check CHECK ((origin = ANY (ARRAY['unknown'::text, 'technical'::text, 'existing'::text])))
);

CREATE TABLE shuttle_identity_audit (
    id bigint NOT NULL,
    user_id uuid NOT NULL,
    actor text NOT NULL,
    revoked boolean NOT NULL,
    changed_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE shuttle_identity_audit_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE shuttle_identity_audit_id_seq OWNED BY shuttle_identity_audit.id;

CREATE TABLE shuttle_inbox (
    bot_id bigint NOT NULL,
    update_id bigint NOT NULL,
    task_id uuid NOT NULL,
    payload jsonb NOT NULL,
    received_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_knowledge_articles (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    event_id uuid,
    slug text NOT NULL,
    title text NOT NULL,
    language text NOT NULL,
    topics text[] DEFAULT '{}'::text[] NOT NULL,
    body text NOT NULL,
    sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    current_revision bigint NOT NULL,
    event_name text,
    event_starts_at timestamp with time zone,
    event_ends_at timestamp with time zone,
    event_timezone text,
    archived_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    updated_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    search_vector tsvector GENERATED ALWAYS AS ((((((setweight(to_tsvector('english'::regconfig, title), 'A'::"char") || setweight(to_tsvector('russian'::regconfig, title), 'A'::"char")) || setweight(to_tsvector('simple'::regconfig, title), 'A'::"char")) || setweight(to_tsvector('english'::regconfig, body), 'B'::"char")) || setweight(to_tsvector('russian'::regconfig, body), 'B'::"char")) || setweight(to_tsvector('simple'::regconfig, body), 'B'::"char"))) STORED,
    CONSTRAINT shuttle_knowledge_articles_body_check CHECK (((octet_length(body) >= 1) AND (octet_length(body) <= 98304))),
    CONSTRAINT shuttle_knowledge_articles_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_articles_check CHECK ((((event_id IS NULL) AND (event_name IS NULL) AND (event_starts_at IS NULL) AND (event_ends_at IS NULL) AND (event_timezone IS NULL) AND (archived_at IS NULL)) OR ((event_id IS NOT NULL) AND (event_name IS NOT NULL) AND (event_starts_at IS NOT NULL) AND (event_ends_at IS NOT NULL) AND (event_timezone IS NOT NULL) AND (event_ends_at > event_starts_at)))),
    CONSTRAINT shuttle_knowledge_articles_check1 CHECK (((archived_at IS NULL) OR (archived_at >= event_ends_at))),
    CONSTRAINT shuttle_knowledge_articles_current_revision_check CHECK ((current_revision > 0)),
    CONSTRAINT shuttle_knowledge_articles_event_name_check CHECK (((char_length(event_name) >= 1) AND (char_length(event_name) <= 256))),
    CONSTRAINT shuttle_knowledge_articles_event_timezone_check CHECK (((char_length(event_timezone) >= 1) AND (char_length(event_timezone) <= 128))),
    CONSTRAINT shuttle_knowledge_articles_language_check CHECK ((language = ANY (ARRAY['en'::text, 'ru'::text]))),
    CONSTRAINT shuttle_knowledge_articles_organization_id_check CHECK (((char_length(organization_id) >= 1) AND (char_length(organization_id) <= 256))),
    CONSTRAINT shuttle_knowledge_articles_slug_check CHECK (((char_length(slug) >= 1) AND (char_length(slug) <= 100))),
    CONSTRAINT shuttle_knowledge_articles_sources_check CHECK (((jsonb_typeof(sources) = 'array'::text) AND (jsonb_array_length(sources) <= 16) AND (octet_length((sources)::text) <= 131072))),
    CONSTRAINT shuttle_knowledge_articles_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT shuttle_knowledge_articles_topics_check CHECK (((cardinality(topics) <= 12) AND (array_position(topics, NULL::text) IS NULL)))
);

CREATE TABLE shuttle_knowledge_moderators (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    user_id uuid NOT NULL,
    enabled boolean NOT NULL,
    version bigint NOT NULL,
    changed_by uuid NOT NULL,
    changed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_knowledge_moderators_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_moderators_organization_id_check CHECK (((char_length(organization_id) >= 1) AND (char_length(organization_id) <= 256))),
    CONSTRAINT shuttle_knowledge_moderators_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_knowledge_suggestions (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    article_id uuid NOT NULL,
    base_revision bigint NOT NULL,
    event_id uuid,
    event_version bigint,
    proposer_id uuid NOT NULL,
    reason text NOT NULL,
    slug text NOT NULL,
    title text NOT NULL,
    language text NOT NULL,
    topics text[] DEFAULT '{}'::text[] NOT NULL,
    body text NOT NULL,
    sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    reviewer_id uuid,
    decision_reason text,
    decided_at timestamp with time zone,
    published_revision bigint,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_knowledge_suggestions_base_revision_check CHECK ((base_revision >= 0)),
    CONSTRAINT shuttle_knowledge_suggestions_body_check CHECK (((octet_length(body) >= 1) AND (octet_length(body) <= 98304))),
    CONSTRAINT shuttle_knowledge_suggestions_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_suggestions_check CHECK (((event_id IS NULL) = (event_version IS NULL))),
    CONSTRAINT shuttle_knowledge_suggestions_check1 CHECK (((reviewer_id IS NULL) OR (reviewer_id <> proposer_id))),
    CONSTRAINT shuttle_knowledge_suggestions_check2 CHECK ((((state = 'pending'::text) AND (reviewer_id IS NULL) AND (decided_at IS NULL) AND (decision_reason IS NULL) AND (published_revision IS NULL)) OR ((state = 'approved'::text) AND (reviewer_id IS NOT NULL) AND (decided_at IS NOT NULL) AND (decision_reason IS NOT NULL) AND (published_revision IS NOT NULL)) OR ((state = 'rejected'::text) AND (reviewer_id IS NOT NULL) AND (decided_at IS NOT NULL) AND (decision_reason IS NOT NULL) AND (published_revision IS NULL)))),
    CONSTRAINT shuttle_knowledge_suggestions_check3 CHECK (((decided_at IS NULL) OR (decided_at >= created_at))),
    CONSTRAINT shuttle_knowledge_suggestions_decision_reason_check CHECK (((char_length(decision_reason) >= 1) AND (char_length(decision_reason) <= 2000))),
    CONSTRAINT shuttle_knowledge_suggestions_event_version_check CHECK ((event_version > 0)),
    CONSTRAINT shuttle_knowledge_suggestions_language_check CHECK ((language = ANY (ARRAY['en'::text, 'ru'::text]))),
    CONSTRAINT shuttle_knowledge_suggestions_organization_id_check CHECK (((char_length(organization_id) >= 1) AND (char_length(organization_id) <= 256))),
    CONSTRAINT shuttle_knowledge_suggestions_published_revision_check CHECK ((published_revision > 0)),
    CONSTRAINT shuttle_knowledge_suggestions_reason_check CHECK (((char_length(reason) >= 1) AND (char_length(reason) <= 2000))),
    CONSTRAINT shuttle_knowledge_suggestions_slug_check CHECK (((char_length(slug) >= 1) AND (char_length(slug) <= 100))),
    CONSTRAINT shuttle_knowledge_suggestions_sources_check CHECK (((jsonb_typeof(sources) = 'array'::text) AND (jsonb_array_length(sources) <= 16) AND (octet_length((sources)::text) <= 131072))),
    CONSTRAINT shuttle_knowledge_suggestions_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_knowledge_suggestions_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT shuttle_knowledge_suggestions_topics_check CHECK (((cardinality(topics) <= 12) AND (array_position(topics, NULL::text) IS NULL)))
);

CREATE TABLE shuttle_users (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    tg_user_id bigint NOT NULL,
    profile jsonb DEFAULT '{}'::jsonb NOT NULL,
    zitadel_subject text DEFAULT ''::text NOT NULL,
    zitadel_org text DEFAULT ''::text NOT NULL,
    revoked boolean DEFAULT false NOT NULL
);

CREATE VIEW shuttle_knowledge_notice_candidates AS
 SELECT s.bot_id,
    s.organization_id,
    s.id AS suggestion_id,
    u.id AS recipient_id,
    s.state AS kind
   FROM ((shuttle_knowledge_suggestions s
     LEFT JOIN shuttle_events e ON (((e.id = s.event_id) AND (e.bot_id = s.bot_id) AND (e.organization_id = s.organization_id))))
     JOIN shuttle_users u ON (((u.bot_id = s.bot_id) AND (u.zitadel_org = s.organization_id) AND (NOT u.revoked) AND (u.zitadel_subject <> ''::text))))
  WHERE (((s.state = ANY (ARRAY['approved'::text, 'rejected'::text])) AND (u.id = s.proposer_id)) OR ((s.state = 'pending'::text) AND (u.id <> s.proposer_id) AND ((((s.event_id IS NULL) OR (e.ends_at <= clock_timestamp()) OR (EXISTS ( SELECT 1
           FROM shuttle_knowledge_articles h
          WHERE ((h.bot_id = s.bot_id) AND (h.organization_id = s.organization_id) AND (h.event_id = s.event_id) AND (h.archived_at IS NOT NULL))))) AND (EXISTS ( SELECT 1
           FROM shuttle_knowledge_moderators m
          WHERE ((m.bot_id = s.bot_id) AND (m.organization_id = s.organization_id) AND (m.user_id = u.id) AND m.enabled)))) OR ((s.event_id IS NOT NULL) AND (e.ends_at > clock_timestamp()) AND (NOT (EXISTS ( SELECT 1
           FROM shuttle_knowledge_articles h
          WHERE ((h.bot_id = s.bot_id) AND (h.organization_id = s.organization_id) AND (h.event_id = s.event_id) AND (h.archived_at IS NOT NULL))))) AND (EXISTS ( SELECT 1
           FROM shuttle_event_authorities a
          WHERE ((a.event_id = s.event_id) AND (a.user_id = u.id) AND (a.role = 'administrator'::text))))))));

CREATE TABLE shuttle_knowledge_notices (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    suggestion_id uuid NOT NULL,
    recipient_id uuid NOT NULL,
    kind text NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    payload jsonb,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_knowledge_notices_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_notices_check CHECK (((state <> 'send_inflight'::text) OR ((attempt_id IS NOT NULL) AND (payload IS NOT NULL)))),
    CONSTRAINT shuttle_knowledge_notices_kind_check CHECK ((kind = ANY (ARRAY['pending'::text, 'approved'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_knowledge_notices_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_knowledge_notices_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_knowledge_operations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    actor_id uuid NOT NULL,
    action_kind text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_knowledge_operations_action_kind_check CHECK ((action_kind = ANY (ARRAY['suggest'::text, 'edit'::text, 'review'::text, 'set_moderator'::text]))),
    CONSTRAINT shuttle_knowledge_operations_actor_check CHECK (((char_length(actor) >= 1) AND (char_length(actor) <= 256))),
    CONSTRAINT shuttle_knowledge_operations_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_operations_operation_id_check CHECK (((char_length(operation_id) >= 1) AND (char_length(operation_id) <= 128))),
    CONSTRAINT shuttle_knowledge_operations_organization_id_check CHECK (((char_length(organization_id) >= 1) AND (char_length(organization_id) <= 256))),
    CONSTRAINT shuttle_knowledge_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_knowledge_operations_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 2097152)))
);

CREATE TABLE shuttle_knowledge_preparations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    actor_id uuid NOT NULL,
    action_kind text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    document jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_knowledge_preparations_action_kind_check CHECK ((action_kind = ANY (ARRAY['suggest'::text, 'edit'::text, 'review'::text, 'set_moderator'::text]))),
    CONSTRAINT shuttle_knowledge_preparations_actor_check CHECK (((char_length(actor) >= 1) AND (char_length(actor) <= 256))),
    CONSTRAINT shuttle_knowledge_preparations_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_preparations_check CHECK ((expires_at > created_at)),
    CONSTRAINT shuttle_knowledge_preparations_document_check CHECK (((jsonb_typeof(document) = 'object'::text) AND (octet_length((document)::text) <= 1048576))),
    CONSTRAINT shuttle_knowledge_preparations_operation_id_check CHECK (((char_length(operation_id) >= 1) AND (char_length(operation_id) <= 128))),
    CONSTRAINT shuttle_knowledge_preparations_organization_id_check CHECK (((char_length(organization_id) >= 1) AND (char_length(organization_id) <= 256))),
    CONSTRAINT shuttle_knowledge_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_knowledge_revisions (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    article_id uuid NOT NULL,
    revision bigint NOT NULL,
    previous_revision bigint,
    change_kind text NOT NULL,
    author_id uuid,
    proposer_id uuid,
    reviewer_id uuid,
    suggestion_id uuid,
    reason text NOT NULL,
    slug text NOT NULL,
    title text NOT NULL,
    language text NOT NULL,
    topics text[] DEFAULT '{}'::text[] NOT NULL,
    body text NOT NULL,
    sources jsonb DEFAULT '[]'::jsonb NOT NULL,
    event_id uuid,
    event_name text,
    event_starts_at timestamp with time zone,
    event_ends_at timestamp with time zone,
    event_timezone text,
    archived_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_knowledge_revisions_body_check CHECK (((octet_length(body) >= 1) AND (octet_length(body) <= 98304))),
    CONSTRAINT shuttle_knowledge_revisions_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_knowledge_revisions_change_kind_check CHECK ((change_kind = ANY (ARRAY['create'::text, 'edit'::text, 'approved_suggestion'::text, 'archive'::text]))),
    CONSTRAINT shuttle_knowledge_revisions_check CHECK ((((revision = 1) AND (previous_revision IS NULL)) OR ((revision > 1) AND (previous_revision IS NOT NULL) AND (previous_revision = (revision - 1))))),
    CONSTRAINT shuttle_knowledge_revisions_check1 CHECK ((((event_id IS NULL) AND (event_name IS NULL) AND (event_starts_at IS NULL) AND (event_ends_at IS NULL) AND (event_timezone IS NULL) AND (archived_at IS NULL)) OR ((event_id IS NOT NULL) AND (event_name IS NOT NULL) AND (event_starts_at IS NOT NULL) AND (event_ends_at IS NOT NULL) AND (event_timezone IS NOT NULL) AND (event_ends_at > event_starts_at)))),
    CONSTRAINT shuttle_knowledge_revisions_check2 CHECK (((archived_at IS NULL) OR (archived_at >= event_ends_at))),
    CONSTRAINT shuttle_knowledge_revisions_check3 CHECK ((((change_kind = 'archive'::text) AND (author_id IS NULL) AND (event_id IS NOT NULL) AND (archived_at IS NOT NULL)) OR ((change_kind <> 'archive'::text) AND (author_id IS NOT NULL)))),
    CONSTRAINT shuttle_knowledge_revisions_check4 CHECK ((((change_kind = 'approved_suggestion'::text) AND (proposer_id IS NOT NULL) AND (reviewer_id IS NOT NULL) AND (proposer_id <> reviewer_id) AND (author_id = reviewer_id) AND (suggestion_id IS NOT NULL)) OR ((change_kind <> 'approved_suggestion'::text) AND (proposer_id IS NULL) AND (reviewer_id IS NULL) AND (suggestion_id IS NULL)))),
    CONSTRAINT shuttle_knowledge_revisions_event_name_check CHECK (((char_length(event_name) >= 1) AND (char_length(event_name) <= 256))),
    CONSTRAINT shuttle_knowledge_revisions_event_timezone_check CHECK (((char_length(event_timezone) >= 1) AND (char_length(event_timezone) <= 128))),
    CONSTRAINT shuttle_knowledge_revisions_language_check CHECK ((language = ANY (ARRAY['en'::text, 'ru'::text]))),
    CONSTRAINT shuttle_knowledge_revisions_organization_id_check CHECK (((char_length(organization_id) >= 1) AND (char_length(organization_id) <= 256))),
    CONSTRAINT shuttle_knowledge_revisions_reason_check CHECK (((char_length(reason) >= 1) AND (char_length(reason) <= 2000))),
    CONSTRAINT shuttle_knowledge_revisions_revision_check CHECK ((revision > 0)),
    CONSTRAINT shuttle_knowledge_revisions_slug_check CHECK (((char_length(slug) >= 1) AND (char_length(slug) <= 100))),
    CONSTRAINT shuttle_knowledge_revisions_sources_check CHECK (((jsonb_typeof(sources) = 'array'::text) AND (jsonb_array_length(sources) <= 16) AND (octet_length((sources)::text) <= 131072))),
    CONSTRAINT shuttle_knowledge_revisions_title_check CHECK (((char_length(title) >= 1) AND (char_length(title) <= 200))),
    CONSTRAINT shuttle_knowledge_revisions_topics_check CHECK (((cardinality(topics) <= 12) AND (array_position(topics, NULL::text) IS NULL)))
);

CREATE TABLE shuttle_legacy_archive_chunks (
    archive_id uuid NOT NULL,
    ordinal integer NOT NULL,
    content bytea NOT NULL,
    CONSTRAINT shuttle_legacy_archive_chunks_content_check CHECK (((octet_length(content) > 0) AND (octet_length(content) <= 262144))),
    CONSTRAINT shuttle_legacy_archive_chunks_ordinal_check CHECK (((ordinal >= 0) AND (ordinal < 1028)))
);

CREATE TABLE shuttle_legacy_archive_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    proposal_id uuid NOT NULL,
    archive_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    receipt jsonb NOT NULL,
    CONSTRAINT shuttle_legacy_archive_operations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_legacy_archive_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    arguments jsonb NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone DEFAULT (clock_timestamp() + '00:05:00'::interval) NOT NULL,
    CONSTRAINT shuttle_legacy_archive_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_legacy_archives (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    sha256 text NOT NULL,
    size_bytes bigint NOT NULL,
    validation jsonb NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_legacy_archives_bot_id_check CHECK (((bot_id > 0) AND (bot_id < '4503599627370496'::bigint))),
    CONSTRAINT shuttle_legacy_archives_organization_id_check CHECK ((organization_id <> ''::text)),
    CONSTRAINT shuttle_legacy_archives_sha256_check CHECK ((sha256 ~ '^[0-9a-f]{64}$'::text)),
    CONSTRAINT shuttle_legacy_archives_size_bytes_check CHECK (((size_bytes > 0) AND (size_bytes <= 269484032)))
);

CREATE TABLE shuttle_legacy_history_applications (
    archive_id uuid NOT NULL,
    projection_version text NOT NULL,
    receipt jsonb NOT NULL
);

CREATE TABLE shuttle_legacy_history_memberships (
    archive_id uuid NOT NULL,
    record_id uuid NOT NULL,
    source_role text NOT NULL,
    source_line integer NOT NULL,
    CONSTRAINT shuttle_legacy_history_memberships_source_line_check CHECK ((source_line > 0)),
    CONSTRAINT shuttle_legacy_history_memberships_source_role_check CHECK ((source_role = ANY (ARRAY['users'::text, 'messages'::text, 'passes'::text, 'files'::text])))
);

CREATE TABLE shuttle_legacy_history_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    proposal_id uuid NOT NULL,
    archive_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    receipt jsonb NOT NULL,
    CONSTRAINT shuttle_legacy_history_operations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_legacy_history_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    arguments jsonb NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone DEFAULT (clock_timestamp() + '00:05:00'::interval) NOT NULL,
    CONSTRAINT shuttle_legacy_history_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_legacy_history_records (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    source_instance text NOT NULL,
    collection_name text NOT NULL,
    source_key text NOT NULL,
    source_role text NOT NULL,
    owner_telegram_id bigint NOT NULL,
    canonical_hash bytea NOT NULL,
    projection_version text NOT NULL,
    projection jsonb,
    first_archive_id uuid NOT NULL,
    first_line integer NOT NULL,
    CONSTRAINT shuttle_legacy_history_records_canonical_hash_check CHECK ((octet_length(canonical_hash) = 32)),
    CONSTRAINT shuttle_legacy_history_records_collection_name_check CHECK ((collection_name ~ '^[A-Za-z0-9_.:-]{1,128}$'::text)),
    CONSTRAINT shuttle_legacy_history_records_first_line_check CHECK ((first_line > 0)),
    CONSTRAINT shuttle_legacy_history_records_owner_telegram_id_check CHECK (((owner_telegram_id > 0) AND (owner_telegram_id < '4503599627370496'::bigint))),
    CONSTRAINT shuttle_legacy_history_records_source_instance_check CHECK ((source_instance ~ '^[A-Za-z0-9_.:-]{1,128}$'::text)),
    CONSTRAINT shuttle_legacy_history_records_source_key_check CHECK (((octet_length(source_key) >= 1) AND (octet_length(source_key) <= 1538))),
    CONSTRAINT shuttle_legacy_history_records_source_role_check CHECK ((source_role = ANY (ARRAY['users'::text, 'messages'::text, 'passes'::text, 'files'::text])))
);

CREATE VIEW shuttle_legacy_history_previews AS
 SELECT id,
    bot_id,
    organization_id,
    owner_telegram_id,
    source_instance,
    collection_name,
    source_key,
    source_role,
    first_archive_id,
    first_line,
    projection_version,
        CASE
            WHEN ((projection ->> 'kind'::text) = 'message'::text) THEN jsonb_build_object('kind', 'message', 'role', (projection -> 'role'::text), 'content', "left"((projection ->> 'content'::text), 256), 'truncated', (length((projection ->> 'content'::text)) > 256), 'recorded_date',
            CASE
                WHEN (octet_length(((projection -> 'recorded_date'::text))::text) <= 512) THEN (projection -> 'recorded_date'::text)
                ELSE 'null'::jsonb
            END, 'recorded_date_omitted', (octet_length(((projection -> 'recorded_date'::text))::text) > 512), 'date_policy', 'preserve_source')
            ELSE projection
        END AS preview
   FROM shuttle_legacy_history_records
  WHERE (projection IS NOT NULL);

CREATE TABLE shuttle_massage_binding_operation_aliases (
    actor_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    canonical_operation_id uuid NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);

CREATE TABLE shuttle_massage_binding_operations (
    actor_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    proposal_id uuid NOT NULL,
    event_id uuid NOT NULL,
    specialist_id text NOT NULL,
    request_hash bytea NOT NULL,
    arguments jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    expires_at timestamp with time zone DEFAULT (clock_timestamp() + '00:05:00'::interval) NOT NULL,
    result jsonb,
    CONSTRAINT shuttle_massage_binding_operations_arguments_check CHECK ((octet_length((arguments)::text) <= 16384)),
    CONSTRAINT shuttle_massage_binding_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_massage_binding_operations_result_check CHECK ((octet_length((result)::text) <= 32768)),
    CONSTRAINT shuttle_massage_binding_operations_specialist_id_check CHECK ((specialist_id ~ '^[a-z][a-z0-9_-]{0,31}$'::text))
);

CREATE TABLE shuttle_massage_bookings (
    id uuid NOT NULL,
    event_id uuid NOT NULL,
    user_id uuid NOT NULL,
    preparation_id uuid NOT NULL,
    window_id text NOT NULL,
    specialist_id text NOT NULL,
    service_id text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    state text DEFAULT 'booked'::text NOT NULL,
    starts_at timestamp with time zone NOT NULL,
    treatment_ends_at timestamp with time zone NOT NULL,
    occupancy_ends_at timestamp with time zone NOT NULL,
    requires_table boolean NOT NULL,
    currency text NOT NULL,
    currency_scale integer NOT NULL,
    price_minor bigint NOT NULL,
    snapshot jsonb NOT NULL,
    source text NOT NULL,
    booked_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    cancelled_at timestamp with time zone,
    CONSTRAINT shuttle_massage_bookings_check CHECK (((starts_at < treatment_ends_at) AND (treatment_ends_at <= occupancy_ends_at))),
    CONSTRAINT shuttle_massage_bookings_check1 CHECK ((((state = 'booked'::text) AND (cancelled_at IS NULL)) OR ((state = 'cancelled'::text) AND (cancelled_at IS NOT NULL)))),
    CONSTRAINT shuttle_massage_bookings_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT shuttle_massage_bookings_currency_scale_check CHECK (((currency_scale >= 0) AND (currency_scale <= 4))),
    CONSTRAINT shuttle_massage_bookings_price_minor_check CHECK (((price_minor >= 0) AND (price_minor <= '9000000000000000'::bigint))),
    CONSTRAINT shuttle_massage_bookings_service_id_check CHECK ((service_id ~ '^[a-z][a-z0-9_-]{0,31}$'::text)),
    CONSTRAINT shuttle_massage_bookings_snapshot_check CHECK ((octet_length((snapshot)::text) <= 32768)),
    CONSTRAINT shuttle_massage_bookings_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text, 'telegram'::text]))),
    CONSTRAINT shuttle_massage_bookings_specialist_id_check CHECK ((specialist_id ~ '^[a-z][a-z0-9_-]{0,31}$'::text)),
    CONSTRAINT shuttle_massage_bookings_state_check CHECK ((state = ANY (ARRAY['booked'::text, 'cancelled'::text]))),
    CONSTRAINT shuttle_massage_bookings_version_check CHECK ((version > 0)),
    CONSTRAINT shuttle_massage_bookings_window_id_check CHECK ((window_id ~ '^[a-z][a-z0-9_-]{0,31}$'::text))
);

CREATE TABLE shuttle_massage_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    kind text NOT NULL,
    source text NOT NULL,
    request_hash bytea NOT NULL,
    preparation_id uuid NOT NULL,
    booking_id uuid NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_massage_operations_kind_check CHECK ((kind = ANY (ARRAY['book'::text, 'cancel'::text, 'instant'::text]))),
    CONSTRAINT shuttle_massage_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_massage_operations_result_check CHECK ((octet_length((result)::text) <= 32768)),
    CONSTRAINT shuttle_massage_operations_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text, 'telegram'::text])))
);

CREATE TABLE shuttle_massage_preparations (
    id uuid NOT NULL,
    event_id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL,
    source text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    request_hash bytea NOT NULL,
    arguments jsonb NOT NULL,
    snapshot jsonb NOT NULL,
    booking_id uuid,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    consumed_result jsonb,
    CONSTRAINT shuttle_massage_preparations_arguments_check CHECK ((octet_length((arguments)::text) <= 16384)),
    CONSTRAINT shuttle_massage_preparations_check CHECK (((kind = ANY (ARRAY['book'::text, 'instant'::text])) OR (booking_id IS NOT NULL))),
    CONSTRAINT shuttle_massage_preparations_check1 CHECK ((((consumed_at IS NULL) AND (consumed_result IS NULL)) OR ((consumed_at IS NOT NULL) AND (consumed_result IS NOT NULL) AND (booking_id IS NOT NULL)))),
    CONSTRAINT shuttle_massage_preparations_consumed_result_check CHECK ((octet_length((consumed_result)::text) <= 32768)),
    CONSTRAINT shuttle_massage_preparations_kind_check CHECK ((kind = ANY (ARRAY['book'::text, 'cancel'::text, 'instant'::text]))),
    CONSTRAINT shuttle_massage_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_massage_preparations_snapshot_check CHECK ((octet_length((snapshot)::text) <= 32768)),
    CONSTRAINT shuttle_massage_preparations_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text, 'telegram'::text])))
);

CREATE TABLE shuttle_massage_reminders (
    id uuid NOT NULL,
    booking_id uuid NOT NULL,
    kind text NOT NULL,
    lead_seconds integer NOT NULL,
    due_at timestamp with time zone NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    payload jsonb,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_massage_reminders_kind_check CHECK ((kind = ANY (ARRAY['owner_long'::text, 'owner_short'::text]))),
    CONSTRAINT shuttle_massage_reminders_lead_seconds_check CHECK ((lead_seconds > 0)),
    CONSTRAINT shuttle_massage_reminders_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_massage_reminders_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_massage_schedule_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_massage_schedule_operations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_massage_schedule_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    arguments jsonb NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_massage_schedule_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_massage_schedules (
    event_id uuid NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    CONSTRAINT shuttle_massage_schedules_document_check CHECK ((octet_length((document)::text) <= 98304)),
    CONSTRAINT shuttle_massage_schedules_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_massage_specialist_bindings (
    event_id uuid NOT NULL,
    specialist_id text NOT NULL,
    user_id uuid NOT NULL,
    active boolean NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    updated_by uuid NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    updated_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_massage_specialist_bindings_specialist_id_check CHECK ((specialist_id ~ '^[a-z][a-z0-9_-]{0,31}$'::text)),
    CONSTRAINT shuttle_massage_specialist_bindings_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_massage_specialist_notifications (
    id uuid NOT NULL,
    booking_id uuid NOT NULL,
    kind text NOT NULL,
    recipient_id uuid NOT NULL,
    binding_version bigint NOT NULL,
    preference_version bigint NOT NULL,
    client_name text NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    payload jsonb,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_massage_specialist_notificatio_preference_version_check CHECK ((preference_version >= 0)),
    CONSTRAINT shuttle_massage_specialist_notifications_binding_version_check CHECK ((binding_version > 0)),
    CONSTRAINT shuttle_massage_specialist_notifications_client_name_check CHECK ((char_length(client_name) <= 256)),
    CONSTRAINT shuttle_massage_specialist_notifications_kind_check CHECK ((kind = ANY (ARRAY['booked'::text, 'cancelled'::text, 'soon'::text]))),
    CONSTRAINT shuttle_massage_specialist_notifications_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_massage_specialist_notifications_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_massage_specialist_preference_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    source text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_massage_specialist_preference_operat_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_massage_specialist_preference_operations_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 4096))),
    CONSTRAINT shuttle_massage_specialist_preference_operations_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text])))
);

CREATE TABLE shuttle_massage_specialist_preferences (
    user_id uuid NOT NULL,
    notify_bookings boolean NOT NULL,
    version bigint NOT NULL,
    notify_next boolean DEFAULT true NOT NULL,
    notify_bookings_version bigint DEFAULT 0 NOT NULL,
    notify_next_version bigint DEFAULT 0 NOT NULL,
    CONSTRAINT shuttle_massage_specialist_preferences_check CHECK (((notify_bookings_version >= 0) AND (notify_bookings_version <= version))),
    CONSTRAINT shuttle_massage_specialist_preferences_check1 CHECK (((notify_next_version >= 0) AND (notify_next_version <= version))),
    CONSTRAINT shuttle_massage_specialist_preferences_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_meal_catalog_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_meal_catalog_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_meal_catalog_operations_result_check CHECK ((octet_length((result)::text) <= 98304))
);

CREATE TABLE shuttle_meal_catalog_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    arguments jsonb NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_meal_catalog_preparations_arguments_check CHECK ((octet_length((arguments)::text) <= 98304)),
    CONSTRAINT shuttle_meal_catalog_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_meal_catalogs (
    event_id uuid NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    CONSTRAINT shuttle_meal_catalogs_document_check CHECK ((octet_length((document)::text) <= 98304)),
    CONSTRAINT shuttle_meal_catalogs_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_meal_draft_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    catalog_version bigint NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_meal_draft_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_meal_draft_operations_result_check CHECK ((octet_length((result)::text) <= 98304))
);

CREATE TABLE shuttle_meal_drafts (
    user_id uuid NOT NULL,
    event_id uuid NOT NULL,
    catalog_version bigint NOT NULL,
    version bigint NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_meal_drafts_result_check CHECK ((octet_length((result)::text) <= 98304)),
    CONSTRAINT shuttle_meal_drafts_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_miniapp_assignment_actions (
    user_id uuid NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    kind text NOT NULL,
    registration_version bigint NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_assignment_actions_kind_check CHECK ((kind = ANY (ARRAY['prepare'::text, 'confirm'::text]))),
    CONSTRAINT shuttle_miniapp_assignment_actions_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128))),
    CONSTRAINT shuttle_miniapp_assignment_actions_registration_version_check CHECK ((registration_version > 0))
);

CREATE TABLE shuttle_miniapp_avatar_resolution_actions (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    proposal_id uuid NOT NULL,
    kind text NOT NULL,
    delivery_version bigint NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_avatar_resolution_action_delivery_version_check CHECK ((delivery_version > 0)),
    CONSTRAINT shuttle_miniapp_avatar_resolution_actions_kind_check CHECK ((kind = ANY (ARRAY['prepare'::text, 'confirm'::text])))
);

CREATE TABLE shuttle_miniapp_details_actions (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    field text NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_details_actions_field_check CHECK ((field = ANY (ARRAY['legal_name'::text, 'passport_number'::text])))
);

CREATE TABLE shuttle_miniapp_meal_draft_actions (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);

CREATE TABLE shuttle_miniapp_order_evidence_actions (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text DEFAULT 'submit'::text NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_order_evidence_actions_kind_check CHECK ((kind = 'submit'::text))
);

CREATE TABLE shuttle_miniapp_orders_selection_actions (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);

CREATE TABLE shuttle_miniapp_pair_assignment_actions (
    user_id uuid NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    kind text NOT NULL,
    pair_version bigint NOT NULL,
    leader_version bigint NOT NULL,
    follower_version bigint NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_pair_assignment_actions_follower_version_check CHECK ((follower_version > 0)),
    CONSTRAINT shuttle_miniapp_pair_assignment_actions_kind_check CHECK ((kind = ANY (ARRAY['prepare'::text, 'confirm'::text]))),
    CONSTRAINT shuttle_miniapp_pair_assignment_actions_leader_version_check CHECK ((leader_version > 0)),
    CONSTRAINT shuttle_miniapp_pair_assignment_actions_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128))),
    CONSTRAINT shuttle_miniapp_pair_assignment_actions_pair_version_check CHECK ((pair_version > 0))
);

CREATE TABLE shuttle_miniapp_profile_actions (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_profile_actions_kind_check CHECK ((kind = ANY (ARRAY['set_name'::text, 'set_language'::text])))
);

CREATE TABLE shuttle_miniapp_sessions (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    user_id uuid NOT NULL,
    proof_hash bytea NOT NULL,
    nonce_hash bytea NOT NULL,
    key_generation bytea NOT NULL,
    bearer_hash bytea,
    state text DEFAULT 'pending'::text NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    organization text DEFAULT ''::text NOT NULL,
    auth_date timestamp with time zone NOT NULL,
    pending_until timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_miniapp_sessions_bearer_hash_check CHECK ((octet_length(bearer_hash) = 32)),
    CONSTRAINT shuttle_miniapp_sessions_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_miniapp_sessions_check CHECK (((state <> 'active'::text) OR ((bearer_hash IS NOT NULL) AND (subject <> ''::text) AND (organization <> ''::text)))),
    CONSTRAINT shuttle_miniapp_sessions_key_generation_check CHECK ((octet_length(key_generation) = 32)),
    CONSTRAINT shuttle_miniapp_sessions_nonce_hash_check CHECK ((octet_length(nonce_hash) = 32)),
    CONSTRAINT shuttle_miniapp_sessions_proof_hash_check CHECK ((octet_length(proof_hash) = 32)),
    CONSTRAINT shuttle_miniapp_sessions_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'active'::text, 'revoked'::text])))
);

CREATE TABLE shuttle_native_payment_evidence_actions (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    user_id uuid NOT NULL,
    completed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);

CREATE TABLE shuttle_notification_resolutions (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    notification_id uuid,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL,
    result jsonb,
    pair_notification_id uuid,
    order_notification_id uuid,
    massage_reminder_id uuid,
    massage_specialist_notification_id uuid,
    knowledge_notice_id uuid,
    CONSTRAINT shuttle_resolution_one_target CHECK (((((((((notification_id IS NOT NULL))::integer + ((pair_notification_id IS NOT NULL))::integer) + ((order_notification_id IS NOT NULL))::integer) + ((massage_reminder_id IS NOT NULL))::integer) + ((massage_specialist_notification_id IS NOT NULL))::integer) + ((knowledge_notice_id IS NOT NULL))::integer) = 1))
);

CREATE TABLE shuttle_order_allocations (
    evidence_id uuid NOT NULL,
    event_id uuid NOT NULL,
    resource_id text NOT NULL,
    units bigint NOT NULL,
    state text NOT NULL,
    CONSTRAINT shuttle_order_allocations_resource_id_check CHECK ((resource_id ~ '^[a-z][a-z0-9_-]{0,31}$'::text)),
    CONSTRAINT shuttle_order_allocations_state_check CHECK ((state = ANY (ARRAY['held'::text, 'occupied'::text, 'released'::text]))),
    CONSTRAINT shuttle_order_allocations_units_check CHECK (((units >= 1) AND (units <= 1000000000)))
);

CREATE TABLE shuttle_order_evidence (
    id uuid NOT NULL,
    order_id uuid NOT NULL,
    event_id uuid NOT NULL,
    user_id uuid NOT NULL,
    file_id uuid NOT NULL,
    source_unique_id text NOT NULL,
    reported_amount_minor bigint NOT NULL,
    quoted_amount_minor bigint NOT NULL,
    currency text NOT NULL,
    currency_scale integer NOT NULL,
    sent_at timestamp with time zone NOT NULL,
    submitted_at timestamp with time zone NOT NULL,
    due_at timestamp with time zone NOT NULL,
    admission_state text NOT NULL,
    reason text NOT NULL,
    state text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    order_kind text DEFAULT 'service'::text NOT NULL,
    CONSTRAINT shuttle_order_evidence_admission_state_check CHECK ((admission_state = ANY (ARRAY['pending'::text, 'exhausted'::text, 'needs_decision'::text]))),
    CONSTRAINT shuttle_order_evidence_check CHECK ((((state = admission_state) AND (version = 1)) OR ((state = ANY (ARRAY['confirmed'::text, 'rejected'::text])) AND (version = 2)))),
    CONSTRAINT shuttle_order_evidence_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT shuttle_order_evidence_currency_scale_check CHECK (((currency_scale >= 0) AND (currency_scale <= 6))),
    CONSTRAINT shuttle_order_evidence_order_kind_check CHECK ((order_kind = ANY (ARRAY['service'::text, 'meal'::text]))),
    CONSTRAINT shuttle_order_evidence_quoted_amount_minor_check CHECK (((quoted_amount_minor >= 0) AND (quoted_amount_minor <= '9000000000000000'::bigint))),
    CONSTRAINT shuttle_order_evidence_reason_check CHECK ((char_length(reason) <= 128)),
    CONSTRAINT shuttle_order_evidence_reported_amount_minor_check CHECK (((reported_amount_minor >= 1) AND (reported_amount_minor <= '9000000000000000'::bigint))),
    CONSTRAINT shuttle_order_evidence_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'exhausted'::text, 'needs_decision'::text, 'confirmed'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_order_evidence_version_check CHECK ((version = ANY (ARRAY[(1)::bigint, (2)::bigint])))
);

CREATE TABLE shuttle_order_evidence_notices (
    id uuid NOT NULL,
    evidence_id uuid NOT NULL,
    recipient_id uuid NOT NULL,
    kind text NOT NULL,
    document jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    payload jsonb,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_order_evidence_notices_document_check CHECK (((jsonb_typeof(document) = 'object'::text) AND (octet_length((document)::text) <= 65536))),
    CONSTRAINT shuttle_order_evidence_notices_kind_check CHECK ((kind = ANY (ARRAY['pending'::text, 'exhausted'::text, 'needs_decision'::text, 'confirmed'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_order_evidence_notices_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_order_evidence_notices_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_order_evidence_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL,
    event_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_order_evidence_operations_kind_check CHECK ((kind = ANY (ARRAY['submit'::text, 'review'::text]))),
    CONSTRAINT shuttle_order_evidence_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_order_evidence_operations_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 65536)))
);

CREATE TABLE shuttle_order_evidence_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    document jsonb NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    presentation text DEFAULT ''::text NOT NULL,
    CONSTRAINT shuttle_order_evidence_preparations_document_check CHECK (((jsonb_typeof(document) = 'object'::text) AND (octet_length((document)::text) <= 65536))),
    CONSTRAINT shuttle_order_evidence_preparations_presentation_check CHECK ((octet_length(presentation) <= 262144)),
    CONSTRAINT shuttle_order_evidence_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_order_evidence_reviews (
    evidence_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    actor_subject text NOT NULL,
    authority_role text NOT NULL,
    decision text NOT NULL,
    amount_minor bigint NOT NULL,
    reason text NOT NULL,
    reviewed_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_order_evidence_reviews_amount_minor_check CHECK (((amount_minor >= 0) AND (amount_minor <= '9000000000000000'::bigint))),
    CONSTRAINT shuttle_order_evidence_reviews_authority_role_check CHECK ((authority_role = ANY (ARRAY['global_administrator'::text, 'ambassador'::text, 'administrator'::text]))),
    CONSTRAINT shuttle_order_evidence_reviews_decision_check CHECK ((decision = ANY (ARRAY['confirmed'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_order_evidence_reviews_reason_check CHECK (((char_length(reason) >= 1) AND (char_length(reason) <= 512)))
);

CREATE TABLE shuttle_order_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL,
    event_id uuid NOT NULL,
    preparation_id uuid,
    order_id uuid,
    business_hash bytea NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    source text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    order_kind text DEFAULT 'service'::text NOT NULL,
    CONSTRAINT shuttle_order_operations_business_hash_check CHECK ((octet_length(business_hash) = 32)),
    CONSTRAINT shuttle_order_operations_check CHECK ((((kind = 'prepare'::text) AND (preparation_id IS NOT NULL) AND (order_id IS NULL)) OR ((kind = 'submit'::text) AND (preparation_id IS NOT NULL) AND (order_id IS NOT NULL)) OR ((kind = 'cancel'::text) AND (preparation_id IS NULL) AND (order_id IS NOT NULL)))),
    CONSTRAINT shuttle_order_operations_kind_check CHECK ((kind = ANY (ARRAY['prepare'::text, 'submit'::text, 'cancel'::text]))),
    CONSTRAINT shuttle_order_operations_order_kind_check CHECK ((order_kind = ANY (ARRAY['service'::text, 'meal'::text]))),
    CONSTRAINT shuttle_order_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_order_operations_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 65536))),
    CONSTRAINT shuttle_order_operations_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text, 'telegram'::text])))
);

CREATE TABLE shuttle_order_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    event_id uuid NOT NULL,
    source_draft_version bigint NOT NULL,
    request_hash bytea NOT NULL,
    arguments jsonb NOT NULL,
    snapshot jsonb NOT NULL,
    created_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    consumed_at timestamp with time zone,
    order_kind text DEFAULT 'service'::text NOT NULL,
    CONSTRAINT shuttle_order_preparations_arguments_check CHECK (((jsonb_typeof(arguments) = 'object'::text) AND (octet_length((arguments)::text) <= 65536))),
    CONSTRAINT shuttle_order_preparations_check CHECK ((expires_at > created_at)),
    CONSTRAINT shuttle_order_preparations_order_kind_check CHECK ((order_kind = ANY (ARRAY['service'::text, 'meal'::text]))),
    CONSTRAINT shuttle_order_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_order_preparations_snapshot_check CHECK (((jsonb_typeof(snapshot) = 'object'::text) AND (octet_length((snapshot)::text) <= 65536))),
    CONSTRAINT shuttle_order_preparations_source_draft_version_check CHECK ((source_draft_version > 0))
);

CREATE TABLE shuttle_order_review_calls (
    actor_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    kind text NOT NULL,
    source text NOT NULL,
    request_hash bytea NOT NULL,
    workspace_id uuid NOT NULL,
    proposal_id uuid,
    expires_at timestamp with time zone,
    document jsonb NOT NULL,
    result jsonb,
    completed_at timestamp with time zone,
    CONSTRAINT shuttle_order_review_calls_check CHECK (((result IS NULL) = (completed_at IS NULL))),
    CONSTRAINT shuttle_order_review_calls_check1 CHECK (((kind = 'commit'::text) OR (result IS NOT NULL))),
    CONSTRAINT shuttle_order_review_calls_check2 CHECK ((((kind = 'commit'::text) AND (proposal_id IS NOT NULL) AND (expires_at IS NOT NULL)) OR ((kind <> 'commit'::text) AND (proposal_id IS NULL) AND (expires_at IS NULL)))),
    CONSTRAINT shuttle_order_review_calls_document_check CHECK (((jsonb_typeof(document) = 'object'::text) AND (octet_length((document)::text) <= 65536))),
    CONSTRAINT shuttle_order_review_calls_kind_check CHECK ((kind = ANY (ARRAY['save'::text, 'cancel'::text, 'commit'::text]))),
    CONSTRAINT shuttle_order_review_calls_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_order_review_calls_result_check CHECK (((jsonb_typeof(result) = 'object'::text) AND (octet_length((result)::text) <= 65536))),
    CONSTRAINT shuttle_order_review_calls_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text])))
);

CREATE TABLE shuttle_order_review_workspaces (
    id uuid NOT NULL,
    actor_id uuid NOT NULL,
    event_id uuid NOT NULL,
    evidence_id uuid NOT NULL,
    revision bigint NOT NULL,
    proposal_id uuid NOT NULL,
    state text NOT NULL,
    CONSTRAINT shuttle_order_review_workspaces_revision_check CHECK ((revision > 0)),
    CONSTRAINT shuttle_order_review_workspaces_state_check CHECK ((state = ANY (ARRAY['prepared'::text, 'cancelled'::text, 'completed'::text])))
);

CREATE TABLE shuttle_orders (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    event_id uuid NOT NULL,
    source_draft_version bigint NOT NULL,
    preparation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    version bigint NOT NULL,
    state text NOT NULL,
    submitted_result jsonb NOT NULL,
    sales_closes_at timestamp with time zone NOT NULL,
    submitted_at timestamp with time zone NOT NULL,
    cancelled_at timestamp with time zone,
    order_kind text DEFAULT 'service'::text NOT NULL,
    CONSTRAINT shuttle_orders_check CHECK ((sales_closes_at > submitted_at)),
    CONSTRAINT shuttle_orders_check1 CHECK ((((state = 'submitted'::text) AND (cancelled_at IS NULL)) OR ((state = 'cancelled'::text) AND (cancelled_at IS NOT NULL)))),
    CONSTRAINT shuttle_orders_order_kind_check CHECK ((order_kind = ANY (ARRAY['service'::text, 'meal'::text]))),
    CONSTRAINT shuttle_orders_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_orders_source_draft_version_check CHECK ((source_draft_version > 0)),
    CONSTRAINT shuttle_orders_state_check CHECK ((state = ANY (ARRAY['submitted'::text, 'cancelled'::text]))),
    CONSTRAINT shuttle_orders_submitted_result_check CHECK (((jsonb_typeof(submitted_result) = 'object'::text) AND (octet_length((submitted_result)::text) <= 65536))),
    CONSTRAINT shuttle_orders_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_orders_catalog_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_orders_catalog_operations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_orders_catalog_preparations (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    arguments jsonb NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_orders_catalog_preparations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_orders_catalogs (
    event_id uuid NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    CONSTRAINT shuttle_orders_catalogs_document_check CHECK ((octet_length((document)::text) <= 98304)),
    CONSTRAINT shuttle_orders_catalogs_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_orders_selection_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    event_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_orders_selection_operations_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_orders_selection_operations_result_check CHECK ((octet_length((result)::text) <= 65536))
);

CREATE TABLE shuttle_orders_selections (
    user_id uuid NOT NULL,
    event_id uuid NOT NULL,
    version bigint NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_orders_selections_result_check CHECK ((octet_length((result)::text) <= 65536)),
    CONSTRAINT shuttle_orders_selections_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_outbox (
    id uuid NOT NULL,
    task_id uuid NOT NULL,
    dedup_key text NOT NULL,
    chunk_seq integer NOT NULL,
    payload jsonb NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    attempt_id uuid,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_pair_assignment_aliases (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    canonical_operation_id text NOT NULL,
    CONSTRAINT shuttle_pair_assignment_aliases_check CHECK ((operation_id <> canonical_operation_id)),
    CONSTRAINT shuttle_pair_assignment_aliases_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128)))
);

CREATE TABLE shuttle_pair_invitations (
    id uuid NOT NULL,
    event_id uuid NOT NULL,
    inviter_registration_id uuid NOT NULL,
    target_user_id uuid NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    created_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    decided_at timestamp with time zone,
    pair_id uuid,
    CONSTRAINT shuttle_pair_invitations_check CHECK ((expires_at > created_at)),
    CONSTRAINT shuttle_pair_invitations_check1 CHECK (((state = 'pending'::text) = (decided_at IS NULL))),
    CONSTRAINT shuttle_pair_invitations_check2 CHECK (((state = 'accepted'::text) = (pair_id IS NOT NULL))),
    CONSTRAINT shuttle_pair_invitations_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'accepted'::text, 'declined'::text, 'revoked'::text, 'expired'::text]))),
    CONSTRAINT shuttle_pair_invitations_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_pair_notifications (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    invitation_id uuid NOT NULL,
    invitation_version bigint NOT NULL,
    recipient_id uuid NOT NULL,
    kind text NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    payload jsonb,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_pair_notifications_invitation_version_check CHECK ((invitation_version > 0)),
    CONSTRAINT shuttle_pair_notifications_kind_check CHECK ((kind = ANY (ARRAY['pending'::text, 'accepted'::text, 'declined'::text, 'revoked'::text, 'expired'::text]))),
    CONSTRAINT shuttle_pair_notifications_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_pair_notifications_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_pair_operations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    kind text NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL
);

CREATE TABLE shuttle_pair_preparations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL
);

CREATE TABLE shuttle_pairs (
    id uuid NOT NULL,
    event_id uuid NOT NULL,
    leader_registration_id uuid NOT NULL,
    follower_registration_id uuid NOT NULL,
    state text DEFAULT 'waitlisted'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    queued_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone NOT NULL,
    assigned_at timestamp with time zone,
    dissolved_at timestamp with time zone,
    CONSTRAINT shuttle_pairs_check CHECK ((leader_registration_id <> follower_registration_id)),
    CONSTRAINT shuttle_pairs_check1 CHECK (((state = 'dissolved'::text) = (dissolved_at IS NOT NULL))),
    CONSTRAINT shuttle_pairs_check2 CHECK (((state <> 'assigned'::text) OR (assigned_at IS NOT NULL))),
    CONSTRAINT shuttle_pairs_state_check CHECK ((state = ANY (ARRAY['waitlisted'::text, 'assigned'::text, 'dissolved'::text]))),
    CONSTRAINT shuttle_pairs_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_pass_deposit_policy (
    pass_id uuid NOT NULL,
    document jsonb NOT NULL
);

CREATE TABLE shuttle_pass_tiers (
    pass_id uuid NOT NULL,
    tier_id text NOT NULL,
    tier_number integer NOT NULL,
    CONSTRAINT shuttle_pass_tiers_tier_id_check CHECK ((tier_id ~ '^[a-z0-9][a-z0-9_-]{0,63}$'::text)),
    CONSTRAINT shuttle_pass_tiers_tier_number_check CHECK (((tier_number >= 1) AND (tier_number <= 100)))
);

CREATE TABLE shuttle_passes (
    id uuid NOT NULL,
    registration_id uuid NOT NULL,
    price_minor bigint NOT NULL,
    currency text NOT NULL,
    currency_scale integer NOT NULL,
    assigned_at timestamp with time zone NOT NULL,
    payment_due_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_passes_check CHECK ((payment_due_at > assigned_at)),
    CONSTRAINT shuttle_passes_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT shuttle_passes_currency_scale_check CHECK (((currency_scale >= 0) AND (currency_scale <= 4))),
    CONSTRAINT shuttle_passes_price_minor_check CHECK (((price_minor >= 0) AND (price_minor <= '9007199254740991'::bigint)))
);

CREATE TABLE shuttle_payment_evidence (
    id uuid NOT NULL,
    event_id uuid NOT NULL,
    pass_id uuid NOT NULL,
    user_id uuid NOT NULL,
    file_id uuid NOT NULL,
    source_unique_id text NOT NULL,
    amount_minor bigint NOT NULL,
    currency text NOT NULL,
    currency_scale integer NOT NULL,
    purpose text NOT NULL,
    sent_at timestamp with time zone NOT NULL,
    submitted_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    due_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_payment_evidence_amount_minor_check CHECK (((amount_minor >= 1) AND (amount_minor <= '9007199254740991'::bigint))),
    CONSTRAINT shuttle_payment_evidence_currency_check CHECK ((currency ~ '^[A-Z]{3}$'::text)),
    CONSTRAINT shuttle_payment_evidence_currency_scale_check CHECK (((currency_scale >= 0) AND (currency_scale <= 4))),
    CONSTRAINT shuttle_payment_evidence_purpose_check CHECK ((purpose = ANY (ARRAY['initial'::text, 'top_up'::text])))
);

CREATE TABLE shuttle_payment_notifications (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    evidence_id uuid NOT NULL,
    recipient_id uuid NOT NULL,
    kind text NOT NULL,
    document jsonb NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    attempt_id uuid,
    payload jsonb,
    receipt jsonb DEFAULT '{}'::jsonb NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_payment_notifications_kind_check CHECK ((kind = ANY (ARRAY['submitted'::text, 'confirmed'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_payment_notifications_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'retryable'::text, 'send_inflight'::text, 'delivered'::text, 'delivery_unknown'::text, 'delivery_failed'::text, 'delivery_blocked'::text, 'dismissed'::text]))),
    CONSTRAINT shuttle_payment_notifications_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_payment_operations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    kind text NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT shuttle_payment_operations_kind_check CHECK ((kind = ANY (ARRAY['submit'::text, 'review'::text])))
);

CREATE TABLE shuttle_payment_preparations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL
);

CREATE TABLE shuttle_payment_review_calls (
    actor_id uuid NOT NULL,
    operation_id text NOT NULL,
    kind text NOT NULL,
    request_hash bytea NOT NULL,
    workspace_id uuid NOT NULL,
    proposal_id uuid,
    expires_at timestamp with time zone,
    result jsonb,
    source text NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT shuttle_payment_review_calls_check CHECK (((result IS NULL) = (completed_at IS NULL))),
    CONSTRAINT shuttle_payment_review_calls_check1 CHECK (((kind = 'commit'::text) OR (result IS NOT NULL))),
    CONSTRAINT shuttle_payment_review_calls_check2 CHECK ((((kind = 'commit'::text) AND (proposal_id IS NOT NULL) AND (expires_at IS NOT NULL)) OR ((kind <> 'commit'::text) AND (proposal_id IS NULL) AND (expires_at IS NULL)))),
    CONSTRAINT shuttle_payment_review_calls_kind_check CHECK ((kind = ANY (ARRAY['prepare'::text, 'cancel'::text, 'commit'::text]))),
    CONSTRAINT shuttle_payment_review_calls_operation_id_check CHECK (((char_length(operation_id) >= 1) AND (char_length(operation_id) <= 128))),
    CONSTRAINT shuttle_payment_review_calls_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_payment_review_calls_source_check CHECK ((source = ANY (ARRAY['agent'::text, 'miniapp'::text])))
);

CREATE TABLE shuttle_payment_review_workspaces (
    id uuid NOT NULL,
    actor_id uuid NOT NULL,
    event_id uuid NOT NULL,
    evidence_id uuid NOT NULL,
    revision bigint NOT NULL,
    proposal_id uuid NOT NULL,
    state text NOT NULL,
    CONSTRAINT shuttle_payment_review_workspaces_revision_check CHECK ((revision > 0)),
    CONSTRAINT shuttle_payment_review_workspaces_state_check CHECK ((state = ANY (ARRAY['prepared'::text, 'cancelled'::text, 'completed'::text])))
);

CREATE TABLE shuttle_payment_reviews (
    id uuid NOT NULL,
    evidence_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    actor_subject text NOT NULL,
    authority_role text NOT NULL,
    decision text NOT NULL,
    amount_minor bigint NOT NULL,
    reason text NOT NULL,
    reviewed_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_payment_reviews_amount_minor_check CHECK (((amount_minor >= 0) AND (amount_minor <= '9007199254740991'::bigint))),
    CONSTRAINT shuttle_payment_reviews_authority_role_check CHECK ((authority_role = ANY (ARRAY['global_administrator'::text, 'ambassador'::text, 'administrator'::text]))),
    CONSTRAINT shuttle_payment_reviews_check CHECK ((((decision = 'confirmed'::text) AND (amount_minor > 0)) OR ((decision = 'rejected'::text) AND (amount_minor = 0) AND (char_length(reason) > 0)))),
    CONSTRAINT shuttle_payment_reviews_decision_check CHECK ((decision = ANY (ARRAY['confirmed'::text, 'rejected'::text]))),
    CONSTRAINT shuttle_payment_reviews_reason_check CHECK ((char_length(reason) <= 512))
);

CREATE TABLE shuttle_payment_versions (
    pass_id uuid NOT NULL,
    version bigint NOT NULL,
    CONSTRAINT shuttle_payment_versions_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_profile_name_sync (
    user_id uuid NOT NULL,
    desired_version bigint NOT NULL,
    synced_version bigint DEFAULT 0 NOT NULL,
    next_attempt_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    updated_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    CONSTRAINT shuttle_profile_name_sync_check CHECK (((synced_version >= 0) AND (synced_version <= desired_version))),
    CONSTRAINT shuttle_profile_name_sync_desired_version_check CHECK ((desired_version > 0))
);

CREATE TABLE shuttle_profile_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_profile_operations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_registration_details (
    user_id uuid NOT NULL,
    version bigint NOT NULL,
    encrypted_record bytea NOT NULL,
    legal_name_present boolean NOT NULL,
    passport_number_present boolean NOT NULL,
    CONSTRAINT shuttle_registration_details_encrypted_record_check CHECK ((octet_length(encrypted_record) >= 29)),
    CONSTRAINT shuttle_registration_details_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_registration_details_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_registration_details_operations_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_registration_details_request_operations (
    user_id uuid NOT NULL,
    operation_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    CONSTRAINT shuttle_registration_details_request_operati_request_hash_check CHECK ((octet_length(request_hash) = 32))
);

CREATE TABLE shuttle_registration_details_requests (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    event_id uuid NOT NULL,
    event_version bigint NOT NULL,
    event_timezone text NOT NULL,
    legal_name_required boolean NOT NULL,
    passport_number_required boolean NOT NULL,
    created_at timestamp with time zone NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    completed_at timestamp with time zone,
    completed_details_version bigint,
    superseded_at timestamp with time zone,
    CONSTRAINT shuttle_registration_details_re_completed_details_version_check CHECK ((completed_details_version >= 0)),
    CONSTRAINT shuttle_registration_details_requests_check CHECK ((expires_at > created_at)),
    CONSTRAINT shuttle_registration_details_requests_check1 CHECK (((NOT passport_number_required) OR legal_name_required)),
    CONSTRAINT shuttle_registration_details_requests_check2 CHECK (((completed_at IS NULL) = (completed_details_version IS NULL))),
    CONSTRAINT shuttle_registration_details_requests_check3 CHECK (((completed_at IS NULL) OR (superseded_at IS NULL))),
    CONSTRAINT shuttle_registration_details_requests_event_version_check CHECK ((event_version > 0))
);

CREATE TABLE shuttle_registration_operations (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    request_hash bytea NOT NULL,
    result jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_registrations (
    id uuid NOT NULL,
    event_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text NOT NULL,
    state text DEFAULT 'waitlisted'::text NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    requested_at timestamp with time zone NOT NULL,
    CONSTRAINT shuttle_registrations_role_check CHECK ((role = ANY (ARRAY['leader'::text, 'follower'::text]))),
    CONSTRAINT shuttle_registrations_state_check CHECK ((state = ANY (ARRAY['waitlisted'::text, 'assigned'::text]))),
    CONSTRAINT shuttle_registrations_version_check CHECK ((version > 0))
);

CREATE TABLE shuttle_run_event_context (
    user_id uuid NOT NULL,
    run_id text NOT NULL,
    event_id uuid NOT NULL,
    CONSTRAINT shuttle_run_event_context_run_id_check CHECK (((char_length(run_id) >= 1) AND (char_length(run_id) <= 128)))
);

CREATE TABLE shuttle_settings (
    id boolean DEFAULT true NOT NULL,
    version bigint NOT NULL,
    document jsonb NOT NULL,
    CONSTRAINT shuttle_settings_id_check CHECK (id)
);

CREATE TABLE shuttle_settings_audit (
    id bigint NOT NULL,
    actor text NOT NULL,
    version bigint NOT NULL,
    old_document jsonb NOT NULL,
    new_document jsonb NOT NULL,
    changed_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE SEQUENCE shuttle_settings_audit_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;

ALTER SEQUENCE shuttle_settings_audit_id_seq OWNED BY shuttle_settings_audit.id;

CREATE TABLE shuttle_settings_change_proposals (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL,
    result jsonb,
    CONSTRAINT shuttle_settings_change_proposals_actor_check CHECK ((actor <> ''::text)),
    CONSTRAINT shuttle_settings_change_proposals_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_settings_change_proposals_document_check CHECK ((jsonb_typeof(document) = 'object'::text)),
    CONSTRAINT shuttle_settings_change_proposals_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128))),
    CONSTRAINT shuttle_settings_change_proposals_organization_id_check CHECK ((organization_id <> ''::text)),
    CONSTRAINT shuttle_settings_change_proposals_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_settings_change_proposals_result_check CHECK ((jsonb_typeof(result) = 'object'::text))
);

CREATE TABLE shuttle_task_closure_proposals (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    task_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL,
    result jsonb,
    CONSTRAINT shuttle_task_closure_proposals_actor_check CHECK ((actor <> ''::text)),
    CONSTRAINT shuttle_task_closure_proposals_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_task_closure_proposals_document_check CHECK ((jsonb_typeof(document) = 'object'::text)),
    CONSTRAINT shuttle_task_closure_proposals_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128))),
    CONSTRAINT shuttle_task_closure_proposals_organization_id_check CHECK ((organization_id <> ''::text)),
    CONSTRAINT shuttle_task_closure_proposals_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_task_closure_proposals_result_check CHECK ((jsonb_typeof(result) = 'object'::text))
);

CREATE TABLE shuttle_task_resolutions (
    task_id uuid NOT NULL,
    operation_id text NOT NULL,
    actor text NOT NULL,
    expected_version bigint NOT NULL,
    resulting_version bigint NOT NULL,
    reason text NOT NULL,
    previous_state text NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);

CREATE TABLE shuttle_tasks (
    id uuid NOT NULL,
    user_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    seq bigint NOT NULL,
    request jsonb NOT NULL,
    state text DEFAULT 'accepted'::text NOT NULL,
    route text DEFAULT ''::text NOT NULL,
    target_task_id uuid DEFAULT '00000000-0000-0000-0000-000000000000'::uuid NOT NULL,
    target_run_id text DEFAULT ''::text NOT NULL,
    run_id text DEFAULT ''::text NOT NULL,
    auth_session_id text DEFAULT ''::text NOT NULL,
    question_id text DEFAULT ''::text NOT NULL,
    snapshot jsonb DEFAULT '{}'::jsonb NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    fence bigint DEFAULT 0 NOT NULL,
    lease_owner text DEFAULT ''::text NOT NULL,
    lease_until timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    response_started_at timestamp with time zone,
    response_first_chunk_seq integer,
    response_last_chunk_seq integer,
    response_delivered_at timestamp with time zone,
    CONSTRAINT shuttle_response_delivered CHECK (((response_delivered_at IS NULL) OR ((response_started_at IS NOT NULL) AND (response_first_chunk_seq IS NOT NULL) AND (response_last_chunk_seq IS NOT NULL)))),
    CONSTRAINT shuttle_response_range CHECK ((((response_first_chunk_seq IS NULL) AND (response_last_chunk_seq IS NULL)) OR ((response_started_at IS NOT NULL) AND (response_first_chunk_seq IS NOT NULL) AND (response_last_chunk_seq IS NOT NULL) AND (response_first_chunk_seq >= 0) AND (response_last_chunk_seq >= response_first_chunk_seq))))
);

CREATE TABLE shuttle_telegram_prelinks (
    id uuid NOT NULL,
    bot_id bigint NOT NULL,
    user_id uuid NOT NULL,
    provider_issuer text NOT NULL,
    assertion_jti text NOT NULL,
    assertion_hash bytea NOT NULL,
    organization text NOT NULL,
    provider_id text NOT NULL,
    external_subject text NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    state text DEFAULT 'pending'::text NOT NULL,
    subject text DEFAULT ''::text NOT NULL,
    created_at timestamp with time zone DEFAULT clock_timestamp() NOT NULL,
    completed_at timestamp with time zone,
    CONSTRAINT shuttle_telegram_prelinks_assertion_hash_check CHECK ((octet_length(assertion_hash) = 32)),
    CONSTRAINT shuttle_telegram_prelinks_assertion_jti_check CHECK (((length(assertion_jti) >= 1) AND (length(assertion_jti) <= 200))),
    CONSTRAINT shuttle_telegram_prelinks_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_telegram_prelinks_check CHECK ((((state = 'pending'::text) AND (subject = ''::text) AND (completed_at IS NULL)) OR ((state = 'completed'::text) AND ((length(subject) >= 1) AND (length(subject) <= 200)) AND (completed_at IS NOT NULL) AND (completed_at <= expires_at)))),
    CONSTRAINT shuttle_telegram_prelinks_expires_at_check CHECK (isfinite(expires_at)),
    CONSTRAINT shuttle_telegram_prelinks_external_subject_check CHECK (((length(external_subject) >= 1) AND (length(external_subject) <= 200))),
    CONSTRAINT shuttle_telegram_prelinks_organization_check CHECK (((length(organization) >= 1) AND (length(organization) <= 200))),
    CONSTRAINT shuttle_telegram_prelinks_provider_id_check CHECK (((length(provider_id) >= 1) AND (length(provider_id) <= 200))),
    CONSTRAINT shuttle_telegram_prelinks_provider_issuer_check CHECK (((length(provider_issuer) >= 1) AND (length(provider_issuer) <= 2048))),
    CONSTRAINT shuttle_telegram_prelinks_state_check CHECK ((state = ANY (ARRAY['pending'::text, 'completed'::text])))
);

CREATE TABLE shuttle_user_event_context (
    user_id uuid NOT NULL,
    event_id uuid,
    version bigint DEFAULT 0 NOT NULL,
    CONSTRAINT shuttle_user_event_context_version_check CHECK ((version >= 0))
);

CREATE TABLE shuttle_user_profiles (
    user_id uuid NOT NULL,
    version bigint NOT NULL,
    full_name text DEFAULT ''::text NOT NULL,
    bot_language text DEFAULT 'auto'::text NOT NULL,
    expectation_id uuid,
    expectation_created_at timestamp with time zone,
    expectation_expires_at timestamp with time zone,
    first_name text DEFAULT ''::text NOT NULL,
    last_name text DEFAULT ''::text NOT NULL,
    name_source text DEFAULT ''::text NOT NULL,
    CONSTRAINT shuttle_profile_name_components CHECK ((((first_name = ''::text) AND (last_name = ''::text) AND (name_source = ''::text)) OR ((first_name <> ''::text) AND (last_name <> ''::text) AND (name_source <> ''::text) AND (full_name = ((first_name || ' '::text) || last_name))))),
    CONSTRAINT shuttle_user_profiles_bot_language_check CHECK ((bot_language = ANY (ARRAY['auto'::text, 'en'::text, 'ru'::text]))),
    CONSTRAINT shuttle_user_profiles_check CHECK ((((expectation_id IS NULL) AND (expectation_created_at IS NULL) AND (expectation_expires_at IS NULL)) OR ((expectation_id IS NOT NULL) AND (expectation_created_at IS NOT NULL) AND (expectation_expires_at IS NOT NULL) AND (expectation_expires_at > expectation_created_at)))),
    CONSTRAINT shuttle_user_profiles_first_name_check CHECK ((char_length(first_name) <= 200)),
    CONSTRAINT shuttle_user_profiles_full_name_check CHECK ((char_length(full_name) <= 256)),
    CONSTRAINT shuttle_user_profiles_last_name_check CHECK ((char_length(last_name) <= 200)),
    CONSTRAINT shuttle_user_profiles_name_source_check CHECK ((name_source = ANY (ARRAY[''::text, 'user'::text, 'iam'::text]))),
    CONSTRAINT shuttle_user_profiles_version_check CHECK ((version >= 0))
);

CREATE TABLE shuttle_user_revocation_proposals (
    bot_id bigint NOT NULL,
    organization_id text NOT NULL,
    actor text NOT NULL,
    operation_id text NOT NULL,
    proposal_id uuid NOT NULL,
    target_id uuid NOT NULL,
    request_hash bytea NOT NULL,
    expires_at timestamp with time zone NOT NULL,
    document jsonb NOT NULL,
    result jsonb,
    CONSTRAINT shuttle_user_revocation_proposals_actor_check CHECK ((actor <> ''::text)),
    CONSTRAINT shuttle_user_revocation_proposals_bot_id_check CHECK ((bot_id > 0)),
    CONSTRAINT shuttle_user_revocation_proposals_document_check CHECK ((jsonb_typeof(document) = 'object'::text)),
    CONSTRAINT shuttle_user_revocation_proposals_operation_id_check CHECK (((length(operation_id) >= 1) AND (length(operation_id) <= 128))),
    CONSTRAINT shuttle_user_revocation_proposals_organization_id_check CHECK ((organization_id <> ''::text)),
    CONSTRAINT shuttle_user_revocation_proposals_request_hash_check CHECK ((octet_length(request_hash) = 32)),
    CONSTRAINT shuttle_user_revocation_proposals_result_check CHECK ((jsonb_typeof(result) = 'object'::text))
);

ALTER TABLE ONLY shuttle_identity_audit ALTER COLUMN id SET DEFAULT nextval('shuttle_identity_audit_id_seq'::regclass);

ALTER TABLE ONLY shuttle_settings_audit ALTER COLUMN id SET DEFAULT nextval('shuttle_settings_audit_id_seq'::regclass);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT order_canonical_identity UNIQUE (user_id, event_id, order_kind, source_draft_version);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT order_identity UNIQUE (id, user_id, event_id, order_kind);

ALTER TABLE ONLY shuttle_order_preparations
    ADD CONSTRAINT order_preparation_identity UNIQUE (id, user_id, event_id, order_kind);

ALTER TABLE ONLY shuttle_active_pair_members
    ADD CONSTRAINT shuttle_active_pair_members_pair_id_role_key UNIQUE (pair_id, role);

ALTER TABLE ONLY shuttle_active_pair_members
    ADD CONSTRAINT shuttle_active_pair_members_pkey PRIMARY KEY (registration_id);

ALTER TABLE ONLY shuttle_assignment_aliases
    ADD CONSTRAINT shuttle_assignment_aliases_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_assignment_preparations
    ADD CONSTRAINT shuttle_assignment_preparations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_assignment_preparations
    ADD CONSTRAINT shuttle_assignment_preparations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_authority_operations
    ADD CONSTRAINT shuttle_authority_operations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_authority_preparations
    ADD CONSTRAINT shuttle_authority_preparations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_authority_preparations
    ADD CONSTRAINT shuttle_authority_preparations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_avatar_deliveries
    ADD CONSTRAINT shuttle_avatar_deliveries_artifact_id_key UNIQUE (artifact_id);

ALTER TABLE ONLY shuttle_avatar_deliveries
    ADD CONSTRAINT shuttle_avatar_deliveries_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_avatar_delivery_resolutions
    ADD CONSTRAINT shuttle_avatar_delivery_resolutions_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_avatar_delivery_resolutions
    ADD CONSTRAINT shuttle_avatar_delivery_resolutions_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_avatar_draft_operations
    ADD CONSTRAINT shuttle_avatar_draft_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_drafts_id_user_id_key UNIQUE (id, user_id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_drafts_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_avatar_resolution_aliases
    ADD CONSTRAINT shuttle_avatar_resolution_aliases_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_delivery_id_key UNIQUE (delivery_id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_id_owner UNIQUE (id, user_id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_conversations
    ADD CONSTRAINT shuttle_conversations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_conversations
    ADD CONSTRAINT shuttle_conversations_user_id_chat_id_key UNIQUE (user_id, chat_id);

ALTER TABLE ONLY shuttle_deployment
    ADD CONSTRAINT shuttle_deployment_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_event_authorities
    ADD CONSTRAINT shuttle_event_authorities_pkey PRIMARY KEY (event_id, user_id);

ALTER TABLE ONLY shuttle_event_change_proposals
    ADD CONSTRAINT shuttle_event_change_proposals_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_event_change_proposals
    ADD CONSTRAINT shuttle_event_change_proposals_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_event_context_operations
    ADD CONSTRAINT shuttle_event_context_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_event_deposit_policy
    ADD CONSTRAINT shuttle_event_deposit_policy_pkey PRIMARY KEY (event_id);

ALTER TABLE ONLY shuttle_event_operations
    ADD CONSTRAINT shuttle_event_operations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_event_registration_rules
    ADD CONSTRAINT shuttle_event_registration_rules_pkey PRIMARY KEY (event_id);

ALTER TABLE ONLY shuttle_events
    ADD CONSTRAINT shuttle_events_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_files
    ADD CONSTRAINT shuttle_files_id_owner UNIQUE (id, user_id);

ALTER TABLE ONLY shuttle_files
    ADD CONSTRAINT shuttle_files_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_history_account_receipts
    ADD CONSTRAINT shuttle_history_account_receipts_pkey PRIMARY KEY (record_id);

ALTER TABLE ONLY shuttle_iam_name_origins
    ADD CONSTRAINT shuttle_iam_name_origins_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY shuttle_identity_audit
    ADD CONSTRAINT shuttle_identity_audit_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_inbox
    ADD CONSTRAINT shuttle_inbox_pkey PRIMARY KEY (bot_id, update_id);

ALTER TABLE ONLY shuttle_inbox
    ADD CONSTRAINT shuttle_inbox_task_id_key UNIQUE (task_id);

ALTER TABLE ONLY shuttle_knowledge_articles
    ADD CONSTRAINT shuttle_knowledge_articles_bot_id_organization_id_id_key UNIQUE (bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_articles
    ADD CONSTRAINT shuttle_knowledge_articles_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_knowledge_moderators
    ADD CONSTRAINT shuttle_knowledge_moderators_pkey PRIMARY KEY (bot_id, organization_id, user_id);

ALTER TABLE ONLY shuttle_knowledge_notices
    ADD CONSTRAINT shuttle_knowledge_notices_bot_id_organization_id_suggestion_key UNIQUE (bot_id, organization_id, suggestion_id, recipient_id, kind);

ALTER TABLE ONLY shuttle_knowledge_notices
    ADD CONSTRAINT shuttle_knowledge_notices_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_knowledge_operations
    ADD CONSTRAINT shuttle_knowledge_operations_pkey PRIMARY KEY (bot_id, organization_id, actor, action_kind, operation_id);

ALTER TABLE ONLY shuttle_knowledge_operations
    ADD CONSTRAINT shuttle_knowledge_operations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_knowledge_preparations
    ADD CONSTRAINT shuttle_knowledge_preparations_pkey PRIMARY KEY (bot_id, organization_id, actor, action_kind, operation_id);

ALTER TABLE ONLY shuttle_knowledge_preparations
    ADD CONSTRAINT shuttle_knowledge_preparations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_pkey PRIMARY KEY (bot_id, organization_id, article_id, revision);

ALTER TABLE ONLY shuttle_knowledge_suggestions
    ADD CONSTRAINT shuttle_knowledge_suggestions_bot_id_organization_id_id_key UNIQUE (bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_suggestions
    ADD CONSTRAINT shuttle_knowledge_suggestions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_legacy_archive_chunks
    ADD CONSTRAINT shuttle_legacy_archive_chunks_pkey PRIMARY KEY (archive_id, ordinal);

ALTER TABLE ONLY shuttle_legacy_archive_operations
    ADD CONSTRAINT shuttle_legacy_archive_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_legacy_archive_preparations
    ADD CONSTRAINT shuttle_legacy_archive_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_legacy_archive_preparations
    ADD CONSTRAINT shuttle_legacy_archive_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_legacy_archives
    ADD CONSTRAINT shuttle_legacy_archives_bot_id_organization_id_sha256_key UNIQUE (bot_id, organization_id, sha256);

ALTER TABLE ONLY shuttle_legacy_archives
    ADD CONSTRAINT shuttle_legacy_archives_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_legacy_history_applications
    ADD CONSTRAINT shuttle_legacy_history_applications_pkey PRIMARY KEY (archive_id);

ALTER TABLE ONLY shuttle_legacy_history_memberships
    ADD CONSTRAINT shuttle_legacy_history_member_archive_id_source_role_source_key UNIQUE (archive_id, source_role, source_line);

ALTER TABLE ONLY shuttle_legacy_history_memberships
    ADD CONSTRAINT shuttle_legacy_history_memberships_pkey PRIMARY KEY (archive_id, record_id);

ALTER TABLE ONLY shuttle_legacy_history_operations
    ADD CONSTRAINT shuttle_legacy_history_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_legacy_history_preparations
    ADD CONSTRAINT shuttle_legacy_history_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_legacy_history_preparations
    ADD CONSTRAINT shuttle_legacy_history_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_legacy_history_records
    ADD CONSTRAINT shuttle_legacy_history_record_bot_id_organization_id_source_key UNIQUE (bot_id, organization_id, source_instance, collection_name, source_key);

ALTER TABLE ONLY shuttle_legacy_history_records
    ADD CONSTRAINT shuttle_legacy_history_records_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_massage_binding_operation_aliases
    ADD CONSTRAINT shuttle_massage_binding_operation_aliases_pkey PRIMARY KEY (actor_id, operation_id);

ALTER TABLE ONLY shuttle_massage_binding_operations
    ADD CONSTRAINT shuttle_massage_binding_operations_pkey PRIMARY KEY (actor_id, operation_id);

ALTER TABLE ONLY shuttle_massage_binding_operations
    ADD CONSTRAINT shuttle_massage_binding_operations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_massage_bookings
    ADD CONSTRAINT shuttle_massage_bookings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_massage_bookings
    ADD CONSTRAINT shuttle_massage_bookings_preparation_id_key UNIQUE (preparation_id);

ALTER TABLE ONLY shuttle_massage_operations
    ADD CONSTRAINT shuttle_massage_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_massage_preparations
    ADD CONSTRAINT shuttle_massage_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_massage_preparations
    ADD CONSTRAINT shuttle_massage_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_massage_reminders
    ADD CONSTRAINT shuttle_massage_reminders_booking_id_kind_key UNIQUE (booking_id, kind);

ALTER TABLE ONLY shuttle_massage_reminders
    ADD CONSTRAINT shuttle_massage_reminders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_massage_schedule_operations
    ADD CONSTRAINT shuttle_massage_schedule_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_massage_schedule_preparations
    ADD CONSTRAINT shuttle_massage_schedule_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_massage_schedule_preparations
    ADD CONSTRAINT shuttle_massage_schedule_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_massage_schedules
    ADD CONSTRAINT shuttle_massage_schedules_pkey PRIMARY KEY (event_id);

ALTER TABLE ONLY shuttle_massage_specialist_bindings
    ADD CONSTRAINT shuttle_massage_specialist_bindings_pkey PRIMARY KEY (event_id, specialist_id);

ALTER TABLE ONLY shuttle_massage_specialist_notifications
    ADD CONSTRAINT shuttle_massage_specialist_notifications_booking_id_kind_key UNIQUE (booking_id, kind);

ALTER TABLE ONLY shuttle_massage_specialist_notifications
    ADD CONSTRAINT shuttle_massage_specialist_notifications_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_massage_specialist_preference_operations
    ADD CONSTRAINT shuttle_massage_specialist_preference_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_massage_specialist_preferences
    ADD CONSTRAINT shuttle_massage_specialist_preferences_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY shuttle_meal_catalog_operations
    ADD CONSTRAINT shuttle_meal_catalog_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_meal_catalog_preparations
    ADD CONSTRAINT shuttle_meal_catalog_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_meal_catalog_preparations
    ADD CONSTRAINT shuttle_meal_catalog_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_meal_catalogs
    ADD CONSTRAINT shuttle_meal_catalogs_pkey PRIMARY KEY (event_id, version);

ALTER TABLE ONLY shuttle_meal_draft_operations
    ADD CONSTRAINT shuttle_meal_draft_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_meal_drafts
    ADD CONSTRAINT shuttle_meal_drafts_pkey PRIMARY KEY (user_id, event_id);

ALTER TABLE ONLY shuttle_miniapp_assignment_actions
    ADD CONSTRAINT shuttle_miniapp_assignment_actions_pkey PRIMARY KEY (user_id, operation_id, kind);

ALTER TABLE ONLY shuttle_miniapp_avatar_resolution_actions
    ADD CONSTRAINT shuttle_miniapp_avatar_resolution_actions_pkey PRIMARY KEY (user_id, operation_id, kind);

ALTER TABLE ONLY shuttle_miniapp_details_actions
    ADD CONSTRAINT shuttle_miniapp_details_actions_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_meal_draft_actions
    ADD CONSTRAINT shuttle_miniapp_meal_draft_actions_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_order_evidence_actions
    ADD CONSTRAINT shuttle_miniapp_order_evidence_actions_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_orders_selection_actions
    ADD CONSTRAINT shuttle_miniapp_orders_selection_actions_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_pair_assignment_actions
    ADD CONSTRAINT shuttle_miniapp_pair_assignment_actions_pkey PRIMARY KEY (user_id, operation_id, kind);

ALTER TABLE ONLY shuttle_miniapp_profile_actions
    ADD CONSTRAINT shuttle_miniapp_profile_actions_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_sessions
    ADD CONSTRAINT shuttle_miniapp_sessions_bot_id_bearer_hash_key UNIQUE (bot_id, bearer_hash);

ALTER TABLE ONLY shuttle_miniapp_sessions
    ADD CONSTRAINT shuttle_miniapp_sessions_bot_id_proof_hash_key UNIQUE (bot_id, proof_hash);

ALTER TABLE ONLY shuttle_miniapp_sessions
    ADD CONSTRAINT shuttle_miniapp_sessions_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_native_payment_evidence_actions
    ADD CONSTRAINT shuttle_native_payment_evidence_actions_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_order_allocations
    ADD CONSTRAINT shuttle_order_allocations_pkey PRIMARY KEY (evidence_id, resource_id);

ALTER TABLE ONLY shuttle_order_evidence_notices
    ADD CONSTRAINT shuttle_order_evidence_notice_evidence_id_recipient_id_kind_key UNIQUE (evidence_id, recipient_id, kind);

ALTER TABLE ONLY shuttle_order_evidence_notices
    ADD CONSTRAINT shuttle_order_evidence_notices_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_order_evidence_operations
    ADD CONSTRAINT shuttle_order_evidence_operations_pkey PRIMARY KEY (user_id, operation_id, kind);

ALTER TABLE ONLY shuttle_order_evidence
    ADD CONSTRAINT shuttle_order_evidence_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_order_evidence_preparations
    ADD CONSTRAINT shuttle_order_evidence_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_order_evidence_preparations
    ADD CONSTRAINT shuttle_order_evidence_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_order_evidence_reviews
    ADD CONSTRAINT shuttle_order_evidence_reviews_pkey PRIMARY KEY (evidence_id);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT shuttle_order_operations_pkey PRIMARY KEY (user_id, operation_id, kind);

ALTER TABLE ONLY shuttle_order_preparations
    ADD CONSTRAINT shuttle_order_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_order_review_calls
    ADD CONSTRAINT shuttle_order_review_calls_pkey PRIMARY KEY (actor_id, operation_id);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspac_actor_id_evidence_id_revision_key UNIQUE (actor_id, evidence_id, revision);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspaces_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspaces_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_orders_catalog_operations
    ADD CONSTRAINT shuttle_orders_catalog_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_orders_catalog_preparations
    ADD CONSTRAINT shuttle_orders_catalog_preparations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_orders_catalog_preparations
    ADD CONSTRAINT shuttle_orders_catalog_preparations_user_id_operation_id_key UNIQUE (user_id, operation_id);

ALTER TABLE ONLY shuttle_orders_catalogs
    ADD CONSTRAINT shuttle_orders_catalogs_pkey PRIMARY KEY (event_id);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT shuttle_orders_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT shuttle_orders_preparation_id_key UNIQUE (preparation_id);

ALTER TABLE ONLY shuttle_orders_selection_operations
    ADD CONSTRAINT shuttle_orders_selection_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_orders_selections
    ADD CONSTRAINT shuttle_orders_selections_pkey PRIMARY KEY (user_id, event_id);

ALTER TABLE ONLY shuttle_outbox
    ADD CONSTRAINT shuttle_outbox_dedup_key_key UNIQUE (dedup_key);

ALTER TABLE ONLY shuttle_outbox
    ADD CONSTRAINT shuttle_outbox_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_outbox
    ADD CONSTRAINT shuttle_outbox_task_id_chunk_seq_key UNIQUE (task_id, chunk_seq);

ALTER TABLE ONLY shuttle_pair_assignment_aliases
    ADD CONSTRAINT shuttle_pair_assignment_aliases_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_pair_invitations
    ADD CONSTRAINT shuttle_pair_invitations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_pair_notifications
    ADD CONSTRAINT shuttle_pair_notifications_invitation_id_invitation_version_key UNIQUE (invitation_id, invitation_version, recipient_id);

ALTER TABLE ONLY shuttle_pair_notifications
    ADD CONSTRAINT shuttle_pair_notifications_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_pair_operations
    ADD CONSTRAINT shuttle_pair_operations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_pair_preparations
    ADD CONSTRAINT shuttle_pair_preparations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_pair_preparations
    ADD CONSTRAINT shuttle_pair_preparations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_pairs
    ADD CONSTRAINT shuttle_pairs_id_event_id_key UNIQUE (id, event_id);

ALTER TABLE ONLY shuttle_pairs
    ADD CONSTRAINT shuttle_pairs_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_pass_deposit_policy
    ADD CONSTRAINT shuttle_pass_deposit_policy_pkey PRIMARY KEY (pass_id);

ALTER TABLE ONLY shuttle_pass_tiers
    ADD CONSTRAINT shuttle_pass_tiers_pkey PRIMARY KEY (pass_id);

ALTER TABLE ONLY shuttle_passes
    ADD CONSTRAINT shuttle_passes_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_passes
    ADD CONSTRAINT shuttle_passes_registration_id_key UNIQUE (registration_id);

ALTER TABLE ONLY shuttle_payment_evidence
    ADD CONSTRAINT shuttle_payment_evidence_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_payment_notifications
    ADD CONSTRAINT shuttle_payment_notifications_evidence_id_recipient_id_kind_key UNIQUE (evidence_id, recipient_id, kind);

ALTER TABLE ONLY shuttle_payment_notifications
    ADD CONSTRAINT shuttle_payment_notifications_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_payment_operations
    ADD CONSTRAINT shuttle_payment_operations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_payment_preparations
    ADD CONSTRAINT shuttle_payment_preparations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_payment_preparations
    ADD CONSTRAINT shuttle_payment_preparations_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_payment_review_calls
    ADD CONSTRAINT shuttle_payment_review_calls_pkey PRIMARY KEY (actor_id, operation_id);

ALTER TABLE ONLY shuttle_payment_review_calls
    ADD CONSTRAINT shuttle_payment_review_calls_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_worksp_actor_id_evidence_id_revision_key UNIQUE (actor_id, evidence_id, revision);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_workspaces_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_workspaces_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_payment_reviews
    ADD CONSTRAINT shuttle_payment_reviews_evidence_id_key UNIQUE (evidence_id);

ALTER TABLE ONLY shuttle_payment_reviews
    ADD CONSTRAINT shuttle_payment_reviews_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_payment_versions
    ADD CONSTRAINT shuttle_payment_versions_pkey PRIMARY KEY (pass_id);

ALTER TABLE ONLY shuttle_profile_name_sync
    ADD CONSTRAINT shuttle_profile_name_sync_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY shuttle_profile_operations
    ADD CONSTRAINT shuttle_profile_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_registration_details_operations
    ADD CONSTRAINT shuttle_registration_details_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_registration_details
    ADD CONSTRAINT shuttle_registration_details_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY shuttle_registration_details_request_operations
    ADD CONSTRAINT shuttle_registration_details_request_operations_pkey PRIMARY KEY (user_id, operation_id);

ALTER TABLE ONLY shuttle_registration_details_requests
    ADD CONSTRAINT shuttle_registration_details_requests_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_registration_operations
    ADD CONSTRAINT shuttle_registration_operations_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_registrations
    ADD CONSTRAINT shuttle_registrations_event_id_user_id_key UNIQUE (event_id, user_id);

ALTER TABLE ONLY shuttle_registrations
    ADD CONSTRAINT shuttle_registrations_id_event UNIQUE (id, event_id);

ALTER TABLE ONLY shuttle_registrations
    ADD CONSTRAINT shuttle_registrations_id_event_role UNIQUE (id, event_id, role);

ALTER TABLE ONLY shuttle_registrations
    ADD CONSTRAINT shuttle_registrations_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_run_event_context
    ADD CONSTRAINT shuttle_run_event_context_pkey PRIMARY KEY (user_id, run_id);

ALTER TABLE ONLY shuttle_settings_audit
    ADD CONSTRAINT shuttle_settings_audit_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_settings_change_proposals
    ADD CONSTRAINT shuttle_settings_change_proposals_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_settings_change_proposals
    ADD CONSTRAINT shuttle_settings_change_proposals_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_settings
    ADD CONSTRAINT shuttle_settings_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_task_closure_proposals
    ADD CONSTRAINT shuttle_task_closure_proposals_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_task_closure_proposals
    ADD CONSTRAINT shuttle_task_closure_proposals_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_task_resolutions
    ADD CONSTRAINT shuttle_task_resolutions_pkey PRIMARY KEY (task_id, operation_id);

ALTER TABLE ONLY shuttle_tasks
    ADD CONSTRAINT shuttle_tasks_conversation_id_seq_key UNIQUE (conversation_id, seq);

ALTER TABLE ONLY shuttle_tasks
    ADD CONSTRAINT shuttle_tasks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_telegram_prelinks
    ADD CONSTRAINT shuttle_telegram_prelinks_pkey PRIMARY KEY (id);

ALTER TABLE ONLY shuttle_telegram_prelinks
    ADD CONSTRAINT shuttle_telegram_prelinks_provider_issuer_assertion_jti_key UNIQUE (provider_issuer, assertion_jti);

ALTER TABLE ONLY shuttle_user_event_context
    ADD CONSTRAINT shuttle_user_event_context_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY shuttle_user_profiles
    ADD CONSTRAINT shuttle_user_profiles_pkey PRIMARY KEY (user_id);

ALTER TABLE ONLY shuttle_user_revocation_proposals
    ADD CONSTRAINT shuttle_user_revocation_proposals_pkey PRIMARY KEY (bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_user_revocation_proposals
    ADD CONSTRAINT shuttle_user_revocation_proposals_proposal_id_key UNIQUE (proposal_id);

ALTER TABLE ONLY shuttle_users
    ADD CONSTRAINT shuttle_users_bot_id_tg_user_id_key UNIQUE (bot_id, tg_user_id);

ALTER TABLE ONLY shuttle_users
    ADD CONSTRAINT shuttle_users_pkey PRIMARY KEY (id);

CREATE INDEX shuttle_avatar_deliveries_ready ON shuttle_avatar_deliveries USING btree (next_attempt_at, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_avatar_draft_native_actions ON shuttle_avatar_draft_operations USING btree (user_id, created_at DESC) WHERE native;

CREATE INDEX shuttle_avatar_drafts_owner ON shuttle_avatar_drafts USING btree (user_id, id);

CREATE INDEX shuttle_avatar_retained_sources ON shuttle_avatar_drafts USING btree (user_id, created_at, id) WHERE (original IS NOT NULL);

CREATE INDEX shuttle_avatars_owner ON shuttle_avatars USING btree (user_id, id);

CREATE UNIQUE INDEX shuttle_details_request_pending ON shuttle_registration_details_requests USING btree (user_id, event_id) WHERE ((completed_at IS NULL) AND (superseded_at IS NULL));

CREATE INDEX shuttle_details_requests_owner ON shuttle_registration_details_requests USING btree (user_id, created_at DESC, id);

CREATE UNIQUE INDEX shuttle_events_knowledge_scope ON shuttle_events USING btree (bot_id, organization_id, id);

CREATE INDEX shuttle_events_scope ON shuttle_events USING btree (bot_id, organization_id, id);

CREATE INDEX shuttle_files_owner ON shuttle_files USING btree (user_id, id);

CREATE INDEX shuttle_knowledge_articles_archive ON shuttle_knowledge_articles USING btree (event_id, id) WHERE ((event_id IS NOT NULL) AND (archived_at IS NULL));

CREATE INDEX shuttle_knowledge_articles_browse ON shuttle_knowledge_articles USING btree (bot_id, organization_id, event_id, language, id);

CREATE UNIQUE INDEX shuttle_knowledge_articles_event_slug ON shuttle_knowledge_articles USING btree (bot_id, organization_id, event_id, language, slug) WHERE (event_id IS NOT NULL);

CREATE UNIQUE INDEX shuttle_knowledge_articles_general_slug ON shuttle_knowledge_articles USING btree (bot_id, organization_id, language, slug) WHERE (event_id IS NULL);

CREATE INDEX shuttle_knowledge_articles_search ON shuttle_knowledge_articles USING gin (search_vector);

CREATE INDEX shuttle_knowledge_articles_topics ON shuttle_knowledge_articles USING gin (topics);

CREATE INDEX shuttle_knowledge_notices_ready ON shuttle_knowledge_notices USING btree (bot_id, organization_id, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_knowledge_notices_recipient ON shuttle_knowledge_notices USING btree (bot_id, organization_id, recipient_id, id);

CREATE UNIQUE INDEX shuttle_knowledge_revisions_archive_once ON shuttle_knowledge_revisions USING btree (bot_id, organization_id, article_id) WHERE (change_kind = 'archive'::text);

CREATE UNIQUE INDEX shuttle_knowledge_revisions_suggestion_once ON shuttle_knowledge_revisions USING btree (bot_id, organization_id, suggestion_id) WHERE (suggestion_id IS NOT NULL);

CREATE INDEX shuttle_knowledge_suggestions_owner ON shuttle_knowledge_suggestions USING btree (bot_id, organization_id, proposer_id, created_at DESC, id);

CREATE INDEX shuttle_knowledge_suggestions_pending ON shuttle_knowledge_suggestions USING btree (bot_id, organization_id, event_id, created_at, id) WHERE (state = 'pending'::text);

CREATE INDEX shuttle_legacy_history_owner ON shuttle_legacy_history_records USING btree (bot_id, organization_id, owner_telegram_id, id);

CREATE INDEX shuttle_massage_active_event_time ON shuttle_massage_bookings USING btree (event_id, starts_at) WHERE (state = 'booked'::text);

CREATE INDEX shuttle_massage_owner_page ON shuttle_massage_bookings USING btree (event_id, user_id, id);

CREATE INDEX shuttle_massage_pending_owner ON shuttle_massage_preparations USING btree (event_id, user_id, id) WHERE (consumed_at IS NULL);

CREATE INDEX shuttle_massage_reminders_due ON shuttle_massage_reminders USING btree (due_at, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_massage_specialist_notifications_due ON shuttle_massage_specialist_notifications USING btree (next_attempt_at, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_massage_staff_events ON shuttle_massage_specialist_bindings USING btree (user_id, event_id) WHERE active;

CREATE INDEX shuttle_miniapp_assignment_actions_recent ON shuttle_miniapp_assignment_actions USING btree (user_id, created_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_avatar_resolution_actions_recent ON shuttle_miniapp_avatar_resolution_actions USING btree (user_id, created_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_details_actions_recent ON shuttle_miniapp_details_actions USING btree (user_id, completed_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_meal_draft_actions_recent ON shuttle_miniapp_meal_draft_actions USING btree (user_id, completed_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_order_evidence_actions_recent ON shuttle_miniapp_order_evidence_actions USING btree (user_id, completed_at DESC, operation_id DESC);

CREATE INDEX shuttle_miniapp_orders_selection_actions_recent ON shuttle_miniapp_orders_selection_actions USING btree (user_id, completed_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_pair_assignment_actions_recent ON shuttle_miniapp_pair_assignment_actions USING btree (user_id, created_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_profile_actions_recent ON shuttle_miniapp_profile_actions USING btree (user_id, completed_at DESC, operation_id);

CREATE INDEX shuttle_miniapp_session_cleanup ON shuttle_miniapp_sessions USING btree (bot_id, auth_date);

CREATE INDEX shuttle_native_payment_evidence_actions_recent ON shuttle_native_payment_evidence_actions USING btree (user_id, completed_at DESC, operation_id DESC);

CREATE INDEX shuttle_order_allocations_usage ON shuttle_order_allocations USING btree (event_id, resource_id) WHERE (state = ANY (ARRAY['held'::text, 'occupied'::text]));

CREATE UNIQUE INDEX shuttle_order_evidence_active ON shuttle_order_evidence USING btree (order_id) WHERE (state <> 'rejected'::text);

CREATE INDEX shuttle_order_evidence_event ON shuttle_order_evidence USING btree (event_id, id);

CREATE INDEX shuttle_order_evidence_notices_ready ON shuttle_order_evidence_notices USING btree (next_attempt_at, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_order_evidence_provenance ON shuttle_order_evidence USING btree (user_id, source_unique_id);

CREATE INDEX shuttle_order_operations_recent ON shuttle_order_operations USING btree (user_id, event_id, created_at DESC, operation_id, kind);

CREATE INDEX shuttle_order_preparations_pending ON shuttle_order_preparations USING btree (user_id, event_id, order_kind, source_draft_version, expires_at DESC) WHERE (consumed_at IS NULL);

CREATE INDEX shuttle_orders_owner_event ON shuttle_orders USING btree (user_id, event_id, id);

CREATE INDEX shuttle_outbox_unresolved_metrics_idx ON shuttle_outbox USING btree (state, created_at) WHERE (state <> ALL (ARRAY['delivered'::text, 'delivery_blocked'::text, 'delivery_failed'::text]));

CREATE INDEX shuttle_pair_invitations_incoming ON shuttle_pair_invitations USING btree (event_id, target_user_id, id);

CREATE UNIQUE INDEX shuttle_pair_invitations_outgoing ON shuttle_pair_invitations USING btree (inviter_registration_id) WHERE (state = 'pending'::text);

CREATE INDEX shuttle_pair_notifications_ready ON shuttle_pair_notifications USING btree (next_attempt_at, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_payment_evidence_event ON shuttle_payment_evidence USING btree (event_id, id);

CREATE INDEX shuttle_payment_evidence_source ON shuttle_payment_evidence USING btree (user_id, source_unique_id);

CREATE INDEX shuttle_payment_notifications_ready ON shuttle_payment_notifications USING btree (next_attempt_at, id) WHERE (state = ANY (ARRAY['pending'::text, 'retryable'::text]));

CREATE INDEX shuttle_payment_review_calls_context ON shuttle_payment_review_calls USING btree (actor_id, completed_at DESC) WHERE ((source = 'miniapp'::text) AND (completed_at IS NOT NULL));

CREATE INDEX shuttle_profile_name_sync_due ON shuttle_profile_name_sync USING btree (next_attempt_at, user_id);

CREATE INDEX shuttle_registrations_queue ON shuttle_registrations USING btree (event_id, state, role, requested_at, id);

CREATE INDEX shuttle_tasks_unresolved_metrics_idx ON shuttle_tasks USING btree (state, created_at) WHERE (state <> ALL (ARRAY['completed'::text, 'failed'::text, 'cancelled'::text, 'delivery_failed'::text, 'closed'::text]));

CREATE UNIQUE INDEX shuttle_user_bound_identity ON shuttle_users USING btree (bot_id, zitadel_org, zitadel_subject) WHERE (zitadel_subject <> ''::text);

CREATE TRIGGER shuttle_assignment_alias_namespace BEFORE INSERT ON shuttle_assignment_aliases FOR EACH ROW EXECUTE FUNCTION shuttle_guard_assignment_namespace();

CREATE TRIGGER shuttle_assignment_preparation_namespace BEFORE INSERT ON shuttle_assignment_preparations FOR EACH ROW EXECUTE FUNCTION shuttle_guard_assignment_namespace();

CREATE TRIGGER shuttle_pair_assignment_alias_namespace BEFORE INSERT ON shuttle_pair_assignment_aliases FOR EACH ROW EXECUTE FUNCTION shuttle_guard_pair_assignment_namespace();

CREATE TRIGGER shuttle_pair_assignment_preparation_namespace BEFORE INSERT ON shuttle_pair_preparations FOR EACH ROW EXECUTE FUNCTION shuttle_guard_pair_assignment_namespace();

CREATE TRIGGER shuttle_pair_receipt_namespace BEFORE INSERT ON shuttle_pair_operations FOR EACH ROW EXECUTE FUNCTION shuttle_guard_pair_assignment_namespace();

CREATE TRIGGER shuttle_registration_receipt_namespace BEFORE INSERT ON shuttle_registration_operations FOR EACH ROW EXECUTE FUNCTION shuttle_guard_assignment_namespace();

ALTER TABLE ONLY shuttle_order_evidence
    ADD CONSTRAINT order_evidence_service_identity FOREIGN KEY (order_id, user_id, event_id, order_kind) REFERENCES shuttle_orders(id, user_id, event_id, order_kind);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT order_operation_order_kind FOREIGN KEY (order_id, user_id, event_id, order_kind) REFERENCES shuttle_orders(id, user_id, event_id, order_kind);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT order_operation_preparation_kind FOREIGN KEY (preparation_id, user_id, event_id, order_kind) REFERENCES shuttle_order_preparations(id, user_id, event_id, order_kind);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT order_preparation_kind FOREIGN KEY (preparation_id, user_id, event_id, order_kind) REFERENCES shuttle_order_preparations(id, user_id, event_id, order_kind);

ALTER TABLE ONLY shuttle_active_pair_members
    ADD CONSTRAINT shuttle_active_pair_members_pair_id_event_id_fkey FOREIGN KEY (pair_id, event_id) REFERENCES shuttle_pairs(id, event_id);

ALTER TABLE ONLY shuttle_active_pair_members
    ADD CONSTRAINT shuttle_active_pair_members_registration_id_event_id_role_fkey FOREIGN KEY (registration_id, event_id, role) REFERENCES shuttle_registrations(id, event_id, role);

ALTER TABLE ONLY shuttle_assignment_aliases
    ADD CONSTRAINT shuttle_assignment_aliases_bot_id_organization_id_actor_ca_fkey FOREIGN KEY (bot_id, organization_id, actor, canonical_operation_id) REFERENCES shuttle_assignment_preparations(bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_avatar_deliveries
    ADD CONSTRAINT shuttle_avatar_deliveries_artifact_id_user_id_fkey FOREIGN KEY (artifact_id, user_id) REFERENCES shuttle_avatars(id, user_id);

ALTER TABLE ONLY shuttle_avatar_delivery_resolutions
    ADD CONSTRAINT shuttle_avatar_delivery_resolutions_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES shuttle_avatar_deliveries(id);

ALTER TABLE ONLY shuttle_avatar_draft_operations
    ADD CONSTRAINT shuttle_avatar_draft_operations_draft_id_user_id_fkey FOREIGN KEY (draft_id, user_id) REFERENCES shuttle_avatar_drafts(id, user_id);

ALTER TABLE ONLY shuttle_avatar_draft_operations
    ADD CONSTRAINT shuttle_avatar_draft_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_drafts_history_archive_id_fkey FOREIGN KEY (history_archive_id) REFERENCES shuttle_legacy_history_applications(archive_id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_drafts_history_record_id_fkey FOREIGN KEY (history_record_id) REFERENCES shuttle_legacy_history_records(id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_drafts_source_file_id_user_id_fkey FOREIGN KEY (source_file_id, user_id) REFERENCES shuttle_files(id, user_id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_drafts_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_avatar_drafts
    ADD CONSTRAINT shuttle_avatar_history_membership FOREIGN KEY (history_archive_id, history_record_id) REFERENCES shuttle_legacy_history_memberships(archive_id, record_id);

ALTER TABLE ONLY shuttle_avatar_resolution_aliases
    ADD CONSTRAINT shuttle_avatar_resolution_ali_bot_id_organization_id_actor_fkey FOREIGN KEY (bot_id, organization_id, actor, canonical_operation_id) REFERENCES shuttle_avatar_delivery_resolutions(bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_delivery_id_fkey FOREIGN KEY (delivery_id) REFERENCES shuttle_outbox(id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_draft_owner FOREIGN KEY (draft_id, user_id) REFERENCES shuttle_avatar_drafts(id, user_id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_source_file_id_fkey FOREIGN KEY (source_file_id) REFERENCES shuttle_files(id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_task_id_fkey FOREIGN KEY (task_id) REFERENCES shuttle_tasks(id);

ALTER TABLE ONLY shuttle_avatars
    ADD CONSTRAINT shuttle_avatars_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_conversations
    ADD CONSTRAINT shuttle_conversations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_event_authorities
    ADD CONSTRAINT shuttle_event_authorities_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_event_authorities
    ADD CONSTRAINT shuttle_event_authorities_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_event_context_operations
    ADD CONSTRAINT shuttle_event_context_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_event_deposit_policy
    ADD CONSTRAINT shuttle_event_deposit_policy_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_event_registration_rules
    ADD CONSTRAINT shuttle_event_registration_rules_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_files
    ADD CONSTRAINT shuttle_files_task_id_fkey FOREIGN KEY (task_id) REFERENCES shuttle_tasks(id);

ALTER TABLE ONLY shuttle_files
    ADD CONSTRAINT shuttle_files_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_history_account_receipts
    ADD CONSTRAINT shuttle_history_account_receipts_record_id_fkey FOREIGN KEY (record_id) REFERENCES shuttle_legacy_history_records(id);

ALTER TABLE ONLY shuttle_history_account_receipts
    ADD CONSTRAINT shuttle_history_account_receipts_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_iam_name_origins
    ADD CONSTRAINT shuttle_iam_name_origins_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_identity_audit
    ADD CONSTRAINT shuttle_identity_audit_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_inbox
    ADD CONSTRAINT shuttle_inbox_task_id_fkey FOREIGN KEY (task_id) REFERENCES shuttle_tasks(id);

ALTER TABLE ONLY shuttle_knowledge_articles
    ADD CONSTRAINT shuttle_knowledge_articles_bot_id_organization_id_event_id_fkey FOREIGN KEY (bot_id, organization_id, event_id) REFERENCES shuttle_events(bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_articles
    ADD CONSTRAINT shuttle_knowledge_articles_current_revision FOREIGN KEY (bot_id, organization_id, id, current_revision) REFERENCES shuttle_knowledge_revisions(bot_id, organization_id, article_id, revision) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY shuttle_knowledge_moderators
    ADD CONSTRAINT shuttle_knowledge_moderators_changed_by_fkey FOREIGN KEY (changed_by) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_moderators
    ADD CONSTRAINT shuttle_knowledge_moderators_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_notices
    ADD CONSTRAINT shuttle_knowledge_notices_bot_id_organization_id_suggestio_fkey FOREIGN KEY (bot_id, organization_id, suggestion_id) REFERENCES shuttle_knowledge_suggestions(bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_notices
    ADD CONSTRAINT shuttle_knowledge_notices_recipient_id_fkey FOREIGN KEY (recipient_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_operations
    ADD CONSTRAINT shuttle_knowledge_operations_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_preparations
    ADD CONSTRAINT shuttle_knowledge_preparations_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_author_id_fkey FOREIGN KEY (author_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_bot_id_organization_id_articl_fkey1 FOREIGN KEY (bot_id, organization_id, article_id, previous_revision) REFERENCES shuttle_knowledge_revisions(bot_id, organization_id, article_id, revision);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_bot_id_organization_id_article_fkey FOREIGN KEY (bot_id, organization_id, article_id) REFERENCES shuttle_knowledge_articles(bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_bot_id_organization_id_event_i_fkey FOREIGN KEY (bot_id, organization_id, event_id) REFERENCES shuttle_events(bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_proposer_id_fkey FOREIGN KEY (proposer_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_revisions
    ADD CONSTRAINT shuttle_knowledge_revisions_suggestion FOREIGN KEY (bot_id, organization_id, suggestion_id) REFERENCES shuttle_knowledge_suggestions(bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_suggestions
    ADD CONSTRAINT shuttle_knowledge_suggestions_bot_id_organization_id_artic_fkey FOREIGN KEY (bot_id, organization_id, article_id, published_revision) REFERENCES shuttle_knowledge_revisions(bot_id, organization_id, article_id, revision);

ALTER TABLE ONLY shuttle_knowledge_suggestions
    ADD CONSTRAINT shuttle_knowledge_suggestions_bot_id_organization_id_event_fkey FOREIGN KEY (bot_id, organization_id, event_id) REFERENCES shuttle_events(bot_id, organization_id, id);

ALTER TABLE ONLY shuttle_knowledge_suggestions
    ADD CONSTRAINT shuttle_knowledge_suggestions_proposer_id_fkey FOREIGN KEY (proposer_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_knowledge_suggestions
    ADD CONSTRAINT shuttle_knowledge_suggestions_reviewer_id_fkey FOREIGN KEY (reviewer_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_legacy_archive_chunks
    ADD CONSTRAINT shuttle_legacy_archive_chunks_archive_id_fkey FOREIGN KEY (archive_id) REFERENCES shuttle_legacy_archives(id) DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE ONLY shuttle_legacy_archive_operations
    ADD CONSTRAINT shuttle_legacy_archive_operations_archive_id_fkey FOREIGN KEY (archive_id) REFERENCES shuttle_legacy_archives(id);

ALTER TABLE ONLY shuttle_legacy_archive_operations
    ADD CONSTRAINT shuttle_legacy_archive_operations_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_legacy_archive_preparations(id);

ALTER TABLE ONLY shuttle_legacy_archive_operations
    ADD CONSTRAINT shuttle_legacy_archive_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_legacy_archive_preparations
    ADD CONSTRAINT shuttle_legacy_archive_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_legacy_archives
    ADD CONSTRAINT shuttle_legacy_archives_created_by_fkey FOREIGN KEY (created_by) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_legacy_history_applications
    ADD CONSTRAINT shuttle_legacy_history_applications_archive_id_fkey FOREIGN KEY (archive_id) REFERENCES shuttle_legacy_archives(id);

ALTER TABLE ONLY shuttle_legacy_history_memberships
    ADD CONSTRAINT shuttle_legacy_history_memberships_archive_id_fkey FOREIGN KEY (archive_id) REFERENCES shuttle_legacy_archives(id);

ALTER TABLE ONLY shuttle_legacy_history_memberships
    ADD CONSTRAINT shuttle_legacy_history_memberships_record_id_fkey FOREIGN KEY (record_id) REFERENCES shuttle_legacy_history_records(id);

ALTER TABLE ONLY shuttle_legacy_history_operations
    ADD CONSTRAINT shuttle_legacy_history_operations_archive_id_fkey FOREIGN KEY (archive_id) REFERENCES shuttle_legacy_history_applications(archive_id);

ALTER TABLE ONLY shuttle_legacy_history_operations
    ADD CONSTRAINT shuttle_legacy_history_operations_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_legacy_history_preparations(id);

ALTER TABLE ONLY shuttle_legacy_history_operations
    ADD CONSTRAINT shuttle_legacy_history_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_legacy_history_preparations
    ADD CONSTRAINT shuttle_legacy_history_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_legacy_history_records
    ADD CONSTRAINT shuttle_legacy_history_records_first_archive_id_fkey FOREIGN KEY (first_archive_id) REFERENCES shuttle_legacy_archives(id);

ALTER TABLE ONLY shuttle_massage_binding_operation_aliases
    ADD CONSTRAINT shuttle_massage_binding_opera_actor_id_canonical_operation_fkey FOREIGN KEY (actor_id, canonical_operation_id) REFERENCES shuttle_massage_binding_operations(actor_id, operation_id);

ALTER TABLE ONLY shuttle_massage_binding_operations
    ADD CONSTRAINT shuttle_massage_binding_operations_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_binding_operations
    ADD CONSTRAINT shuttle_massage_binding_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_bookings
    ADD CONSTRAINT shuttle_massage_bookings_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_bookings
    ADD CONSTRAINT shuttle_massage_bookings_preparation_id_fkey FOREIGN KEY (preparation_id) REFERENCES shuttle_massage_preparations(id);

ALTER TABLE ONLY shuttle_massage_bookings
    ADD CONSTRAINT shuttle_massage_bookings_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_operations
    ADD CONSTRAINT shuttle_massage_operations_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES shuttle_massage_bookings(id);

ALTER TABLE ONLY shuttle_massage_operations
    ADD CONSTRAINT shuttle_massage_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_operations
    ADD CONSTRAINT shuttle_massage_operations_preparation_id_fkey FOREIGN KEY (preparation_id) REFERENCES shuttle_massage_preparations(id);

ALTER TABLE ONLY shuttle_massage_operations
    ADD CONSTRAINT shuttle_massage_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_preparations
    ADD CONSTRAINT shuttle_massage_preparation_booking_fk FOREIGN KEY (booking_id) REFERENCES shuttle_massage_bookings(id);

ALTER TABLE ONLY shuttle_massage_preparations
    ADD CONSTRAINT shuttle_massage_preparations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_preparations
    ADD CONSTRAINT shuttle_massage_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_reminders
    ADD CONSTRAINT shuttle_massage_reminders_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES shuttle_massage_bookings(id);

ALTER TABLE ONLY shuttle_massage_schedule_operations
    ADD CONSTRAINT shuttle_massage_schedule_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_schedule_preparations
    ADD CONSTRAINT shuttle_massage_schedule_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_schedules
    ADD CONSTRAINT shuttle_massage_schedules_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_specialist_bindings
    ADD CONSTRAINT shuttle_massage_specialist_bindings_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_specialist_bindings
    ADD CONSTRAINT shuttle_massage_specialist_bindings_updated_by_fkey FOREIGN KEY (updated_by) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_specialist_bindings
    ADD CONSTRAINT shuttle_massage_specialist_bindings_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_specialist_notifications
    ADD CONSTRAINT shuttle_massage_specialist_notifications_booking_id_fkey FOREIGN KEY (booking_id) REFERENCES shuttle_massage_bookings(id);

ALTER TABLE ONLY shuttle_massage_specialist_notifications
    ADD CONSTRAINT shuttle_massage_specialist_notifications_recipient_id_fkey FOREIGN KEY (recipient_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_specialist_preference_operations
    ADD CONSTRAINT shuttle_massage_specialist_preference_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_massage_specialist_preference_operations
    ADD CONSTRAINT shuttle_massage_specialist_preference_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_massage_specialist_preferences
    ADD CONSTRAINT shuttle_massage_specialist_preferences_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_meal_catalog_operations
    ADD CONSTRAINT shuttle_meal_catalog_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_meal_catalog_operations
    ADD CONSTRAINT shuttle_meal_catalog_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_meal_catalog_preparations
    ADD CONSTRAINT shuttle_meal_catalog_preparations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_meal_catalog_preparations
    ADD CONSTRAINT shuttle_meal_catalog_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_meal_catalogs
    ADD CONSTRAINT shuttle_meal_catalogs_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_meal_draft_operations
    ADD CONSTRAINT shuttle_meal_draft_operations_event_id_catalog_version_fkey FOREIGN KEY (event_id, catalog_version) REFERENCES shuttle_meal_catalogs(event_id, version);

ALTER TABLE ONLY shuttle_meal_draft_operations
    ADD CONSTRAINT shuttle_meal_draft_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_meal_drafts
    ADD CONSTRAINT shuttle_meal_drafts_event_id_catalog_version_fkey FOREIGN KEY (event_id, catalog_version) REFERENCES shuttle_meal_catalogs(event_id, version);

ALTER TABLE ONLY shuttle_meal_drafts
    ADD CONSTRAINT shuttle_meal_drafts_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_miniapp_assignment_actions
    ADD CONSTRAINT shuttle_miniapp_assignment_actions_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_assignment_preparations(proposal_id);

ALTER TABLE ONLY shuttle_miniapp_assignment_actions
    ADD CONSTRAINT shuttle_miniapp_assignment_actions_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_miniapp_avatar_resolution_actions
    ADD CONSTRAINT shuttle_miniapp_avatar_resolution_actions_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_avatar_delivery_resolutions(proposal_id);

ALTER TABLE ONLY shuttle_miniapp_avatar_resolution_actions
    ADD CONSTRAINT shuttle_miniapp_avatar_resolution_actions_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_miniapp_details_actions
    ADD CONSTRAINT shuttle_miniapp_details_actions_user_id_operation_id_fkey FOREIGN KEY (user_id, operation_id) REFERENCES shuttle_registration_details_operations(user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_meal_draft_actions
    ADD CONSTRAINT shuttle_miniapp_meal_draft_actions_user_id_operation_id_fkey FOREIGN KEY (user_id, operation_id) REFERENCES shuttle_meal_draft_operations(user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_order_evidence_actions
    ADD CONSTRAINT shuttle_miniapp_order_evidence_a_user_id_operation_id_kind_fkey FOREIGN KEY (user_id, operation_id, kind) REFERENCES shuttle_order_evidence_operations(user_id, operation_id, kind);

ALTER TABLE ONLY shuttle_miniapp_orders_selection_actions
    ADD CONSTRAINT shuttle_miniapp_orders_selection_acti_user_id_operation_id_fkey FOREIGN KEY (user_id, operation_id) REFERENCES shuttle_orders_selection_operations(user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_pair_assignment_actions
    ADD CONSTRAINT shuttle_miniapp_pair_assignment_actions_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_pair_preparations(proposal_id);

ALTER TABLE ONLY shuttle_miniapp_pair_assignment_actions
    ADD CONSTRAINT shuttle_miniapp_pair_assignment_actions_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_miniapp_profile_actions
    ADD CONSTRAINT shuttle_miniapp_profile_actions_user_id_operation_id_fkey FOREIGN KEY (user_id, operation_id) REFERENCES shuttle_profile_operations(user_id, operation_id);

ALTER TABLE ONLY shuttle_miniapp_sessions
    ADD CONSTRAINT shuttle_miniapp_sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_native_payment_evidence_actions
    ADD CONSTRAINT shuttle_native_payment_eviden_bot_id_organization_id_actor_fkey FOREIGN KEY (bot_id, organization_id, actor, operation_id) REFERENCES shuttle_payment_operations(bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_native_payment_evidence_actions
    ADD CONSTRAINT shuttle_native_payment_evidence_actions_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resoluti_massage_specialist_notificat_fkey FOREIGN KEY (massage_specialist_notification_id) REFERENCES shuttle_massage_specialist_notifications(id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_knowledge_notice_id_fkey FOREIGN KEY (knowledge_notice_id) REFERENCES shuttle_knowledge_notices(id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_massage_reminder_id_fkey FOREIGN KEY (massage_reminder_id) REFERENCES shuttle_massage_reminders(id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_notification_id_fkey FOREIGN KEY (notification_id) REFERENCES shuttle_payment_notifications(id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_order_notification_id_fkey FOREIGN KEY (order_notification_id) REFERENCES shuttle_order_evidence_notices(id);

ALTER TABLE ONLY shuttle_notification_resolutions
    ADD CONSTRAINT shuttle_notification_resolutions_pair_notification_id_fkey FOREIGN KEY (pair_notification_id) REFERENCES shuttle_pair_notifications(id);

ALTER TABLE ONLY shuttle_order_allocations
    ADD CONSTRAINT shuttle_order_allocations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_allocations
    ADD CONSTRAINT shuttle_order_allocations_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_order_evidence(id);

ALTER TABLE ONLY shuttle_order_evidence
    ADD CONSTRAINT shuttle_order_evidence_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_evidence
    ADD CONSTRAINT shuttle_order_evidence_file_id_fkey FOREIGN KEY (file_id) REFERENCES shuttle_files(id);

ALTER TABLE ONLY shuttle_order_evidence_notices
    ADD CONSTRAINT shuttle_order_evidence_notices_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_order_evidence(id);

ALTER TABLE ONLY shuttle_order_evidence_notices
    ADD CONSTRAINT shuttle_order_evidence_notices_recipient_id_fkey FOREIGN KEY (recipient_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_evidence_operations
    ADD CONSTRAINT shuttle_order_evidence_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_evidence_operations
    ADD CONSTRAINT shuttle_order_evidence_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_evidence
    ADD CONSTRAINT shuttle_order_evidence_order_id_fkey FOREIGN KEY (order_id) REFERENCES shuttle_orders(id);

ALTER TABLE ONLY shuttle_order_evidence_preparations
    ADD CONSTRAINT shuttle_order_evidence_preparations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_evidence_preparations
    ADD CONSTRAINT shuttle_order_evidence_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_evidence_reviews
    ADD CONSTRAINT shuttle_order_evidence_reviews_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_evidence_reviews
    ADD CONSTRAINT shuttle_order_evidence_reviews_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_order_evidence(id);

ALTER TABLE ONLY shuttle_order_evidence
    ADD CONSTRAINT shuttle_order_evidence_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT shuttle_order_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT shuttle_order_operations_order_id_fkey FOREIGN KEY (order_id) REFERENCES shuttle_orders(id);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT shuttle_order_operations_preparation_id_fkey FOREIGN KEY (preparation_id) REFERENCES shuttle_order_preparations(id);

ALTER TABLE ONLY shuttle_order_operations
    ADD CONSTRAINT shuttle_order_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_preparations
    ADD CONSTRAINT shuttle_order_preparations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_preparations
    ADD CONSTRAINT shuttle_order_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_review_calls
    ADD CONSTRAINT shuttle_order_review_calls_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_review_calls
    ADD CONSTRAINT shuttle_order_review_calls_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES shuttle_order_review_workspaces(id);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspaces_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspaces_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspaces_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_order_evidence(id);

ALTER TABLE ONLY shuttle_order_review_workspaces
    ADD CONSTRAINT shuttle_order_review_workspaces_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_order_evidence_preparations(id);

ALTER TABLE ONLY shuttle_orders_catalog_operations
    ADD CONSTRAINT shuttle_orders_catalog_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_orders_catalog_preparations
    ADD CONSTRAINT shuttle_orders_catalog_preparations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_orders_catalogs
    ADD CONSTRAINT shuttle_orders_catalogs_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT shuttle_orders_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT shuttle_orders_preparation_id_fkey FOREIGN KEY (preparation_id) REFERENCES shuttle_order_preparations(id);

ALTER TABLE ONLY shuttle_orders_selection_operations
    ADD CONSTRAINT shuttle_orders_selection_operations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_orders_selection_operations
    ADD CONSTRAINT shuttle_orders_selection_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_orders_selections
    ADD CONSTRAINT shuttle_orders_selections_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_orders_selections
    ADD CONSTRAINT shuttle_orders_selections_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_orders
    ADD CONSTRAINT shuttle_orders_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_outbox
    ADD CONSTRAINT shuttle_outbox_task_id_fkey FOREIGN KEY (task_id) REFERENCES shuttle_tasks(id);

ALTER TABLE ONLY shuttle_pair_assignment_aliases
    ADD CONSTRAINT shuttle_pair_assignment_alias_bot_id_organization_id_actor_fkey FOREIGN KEY (bot_id, organization_id, actor, canonical_operation_id) REFERENCES shuttle_pair_preparations(bot_id, organization_id, actor, operation_id);

ALTER TABLE ONLY shuttle_pair_invitations
    ADD CONSTRAINT shuttle_pair_invitations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_pair_invitations
    ADD CONSTRAINT shuttle_pair_invitations_inviter_registration_id_event_id_fkey FOREIGN KEY (inviter_registration_id, event_id) REFERENCES shuttle_registrations(id, event_id);

ALTER TABLE ONLY shuttle_pair_invitations
    ADD CONSTRAINT shuttle_pair_invitations_pair_id_event_id_fkey FOREIGN KEY (pair_id, event_id) REFERENCES shuttle_pairs(id, event_id);

ALTER TABLE ONLY shuttle_pair_invitations
    ADD CONSTRAINT shuttle_pair_invitations_target_user_id_fkey FOREIGN KEY (target_user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_pair_notifications
    ADD CONSTRAINT shuttle_pair_notifications_invitation_id_fkey FOREIGN KEY (invitation_id) REFERENCES shuttle_pair_invitations(id);

ALTER TABLE ONLY shuttle_pair_notifications
    ADD CONSTRAINT shuttle_pair_notifications_recipient_id_fkey FOREIGN KEY (recipient_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_pairs
    ADD CONSTRAINT shuttle_pairs_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_pairs
    ADD CONSTRAINT shuttle_pairs_follower_registration_id_event_id_fkey FOREIGN KEY (follower_registration_id, event_id) REFERENCES shuttle_registrations(id, event_id);

ALTER TABLE ONLY shuttle_pairs
    ADD CONSTRAINT shuttle_pairs_leader_registration_id_event_id_fkey FOREIGN KEY (leader_registration_id, event_id) REFERENCES shuttle_registrations(id, event_id);

ALTER TABLE ONLY shuttle_pass_deposit_policy
    ADD CONSTRAINT shuttle_pass_deposit_policy_pass_id_fkey FOREIGN KEY (pass_id) REFERENCES shuttle_passes(id);

ALTER TABLE ONLY shuttle_pass_tiers
    ADD CONSTRAINT shuttle_pass_tiers_pass_id_fkey FOREIGN KEY (pass_id) REFERENCES shuttle_passes(id);

ALTER TABLE ONLY shuttle_passes
    ADD CONSTRAINT shuttle_passes_registration_id_fkey FOREIGN KEY (registration_id) REFERENCES shuttle_registrations(id);

ALTER TABLE ONLY shuttle_payment_evidence
    ADD CONSTRAINT shuttle_payment_evidence_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_payment_evidence
    ADD CONSTRAINT shuttle_payment_evidence_file_id_fkey FOREIGN KEY (file_id) REFERENCES shuttle_files(id);

ALTER TABLE ONLY shuttle_payment_evidence
    ADD CONSTRAINT shuttle_payment_evidence_pass_id_fkey FOREIGN KEY (pass_id) REFERENCES shuttle_passes(id);

ALTER TABLE ONLY shuttle_payment_evidence
    ADD CONSTRAINT shuttle_payment_evidence_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_payment_notifications
    ADD CONSTRAINT shuttle_payment_notifications_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_payment_evidence(id);

ALTER TABLE ONLY shuttle_payment_notifications
    ADD CONSTRAINT shuttle_payment_notifications_recipient_id_fkey FOREIGN KEY (recipient_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_payment_review_calls
    ADD CONSTRAINT shuttle_payment_review_calls_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_payment_review_calls
    ADD CONSTRAINT shuttle_payment_review_calls_workspace_id_fkey FOREIGN KEY (workspace_id) REFERENCES shuttle_payment_review_workspaces(id);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_workspaces_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_workspaces_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_workspaces_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_payment_evidence(id);

ALTER TABLE ONLY shuttle_payment_review_workspaces
    ADD CONSTRAINT shuttle_payment_review_workspaces_proposal_id_fkey FOREIGN KEY (proposal_id) REFERENCES shuttle_payment_preparations(proposal_id);

ALTER TABLE ONLY shuttle_payment_reviews
    ADD CONSTRAINT shuttle_payment_reviews_actor_id_fkey FOREIGN KEY (actor_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_payment_reviews
    ADD CONSTRAINT shuttle_payment_reviews_evidence_id_fkey FOREIGN KEY (evidence_id) REFERENCES shuttle_payment_evidence(id);

ALTER TABLE ONLY shuttle_payment_versions
    ADD CONSTRAINT shuttle_payment_versions_pass_id_fkey FOREIGN KEY (pass_id) REFERENCES shuttle_passes(id);

ALTER TABLE ONLY shuttle_profile_name_sync
    ADD CONSTRAINT shuttle_profile_name_sync_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_user_profiles(user_id);

ALTER TABLE ONLY shuttle_profile_operations
    ADD CONSTRAINT shuttle_profile_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_registration_details_operations
    ADD CONSTRAINT shuttle_registration_details_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_registration_details_request_operations
    ADD CONSTRAINT shuttle_registration_details_request_operations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_registration_details_requests
    ADD CONSTRAINT shuttle_registration_details_requests_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_registration_details_requests
    ADD CONSTRAINT shuttle_registration_details_requests_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_registration_details
    ADD CONSTRAINT shuttle_registration_details_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_registrations
    ADD CONSTRAINT shuttle_registrations_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_registrations
    ADD CONSTRAINT shuttle_registrations_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_run_event_context
    ADD CONSTRAINT shuttle_run_event_context_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_run_event_context
    ADD CONSTRAINT shuttle_run_event_context_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_task_closure_proposals
    ADD CONSTRAINT shuttle_task_closure_proposals_task_id_fkey FOREIGN KEY (task_id) REFERENCES shuttle_tasks(id);

ALTER TABLE ONLY shuttle_task_resolutions
    ADD CONSTRAINT shuttle_task_resolutions_task_id_fkey FOREIGN KEY (task_id) REFERENCES shuttle_tasks(id);

ALTER TABLE ONLY shuttle_tasks
    ADD CONSTRAINT shuttle_tasks_conversation_id_fkey FOREIGN KEY (conversation_id) REFERENCES shuttle_conversations(id);

ALTER TABLE ONLY shuttle_tasks
    ADD CONSTRAINT shuttle_tasks_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_telegram_prelinks
    ADD CONSTRAINT shuttle_telegram_prelinks_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_user_event_context
    ADD CONSTRAINT shuttle_user_event_context_event_id_fkey FOREIGN KEY (event_id) REFERENCES shuttle_events(id);

ALTER TABLE ONLY shuttle_user_event_context
    ADD CONSTRAINT shuttle_user_event_context_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_user_profiles
    ADD CONSTRAINT shuttle_user_profiles_user_id_fkey FOREIGN KEY (user_id) REFERENCES shuttle_users(id);

ALTER TABLE ONLY shuttle_user_revocation_proposals
    ADD CONSTRAINT shuttle_user_revocation_proposals_target_id_fkey FOREIGN KEY (target_id) REFERENCES shuttle_users(id);

INSERT INTO shuttle_settings (id, version, document) VALUES (true, 1, '{"default_language": "en", "max_parallel_tasks": 8, "auth_cache_ttl_seconds": 0, "question_timeout_seconds": 3600}');
-- END PRESERVED VERSION 73 DDL
 ELSIF applied<>73 THEN
  RAISE EXCEPTION 'partial legacy schema: finish migrations through version 73 with the previous release';
 END IF;
END $shuttle_baseline74$;
-- +goose StatementEnd
-- BEGIN COMMON VERSION 74 DDL
ALTER TABLE shuttle_user_profiles
 ADD COLUMN first_name_latin text NOT NULL DEFAULT '',
 ADD COLUMN last_name_latin text NOT NULL DEFAULT '';
-- +goose StatementBegin
CREATE FUNCTION shuttle_clear_stale_latin_name() RETURNS trigger
 LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.first_name IS DISTINCT FROM OLD.first_name OR NEW.last_name IS DISTINCT FROM OLD.last_name THEN
  NEW.first_name_latin := '';
  NEW.last_name_latin := '';
 END IF;
 RETURN NEW;
END $$;
-- +goose StatementEnd
CREATE TRIGGER shuttle_clear_stale_latin_name
 BEFORE UPDATE OF first_name,last_name ON shuttle_user_profiles
 FOR EACH ROW EXECUTE FUNCTION shuttle_clear_stale_latin_name();
-- END COMMON VERSION 74 DDL
-- +goose Down
-- Rollback removes only an unused baseline; preserve accepted data before rollback.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM goose_db_version WHERE version_id > 0 AND version_id <= 73) THEN
  RAISE EXCEPTION 'legacy migration history must be preserved; baseline rollback requires a fresh database';
 END IF;
END $$;
-- +goose StatementEnd
LOCK TABLE shuttle_active_pair_members, shuttle_assignment_aliases, shuttle_assignment_preparations, shuttle_authority_operations, shuttle_authority_preparations, shuttle_avatar_deliveries, shuttle_avatar_delivery_resolutions, shuttle_avatar_draft_operations, shuttle_avatar_drafts, shuttle_avatar_resolution_aliases, shuttle_avatars, shuttle_conversations, shuttle_deployment, shuttle_event_authorities, shuttle_event_change_proposals, shuttle_event_context_operations, shuttle_event_deposit_policy, shuttle_event_operations, shuttle_event_registration_rules, shuttle_events, shuttle_files, shuttle_history_account_receipts, shuttle_iam_name_origins, shuttle_identity_audit, shuttle_inbox, shuttle_knowledge_articles, shuttle_knowledge_moderators, shuttle_knowledge_suggestions, shuttle_users, shuttle_knowledge_notices, shuttle_knowledge_operations, shuttle_knowledge_preparations, shuttle_knowledge_revisions, shuttle_legacy_archive_chunks, shuttle_legacy_archive_operations, shuttle_legacy_archive_preparations, shuttle_legacy_archives, shuttle_legacy_history_applications, shuttle_legacy_history_memberships, shuttle_legacy_history_operations, shuttle_legacy_history_preparations, shuttle_legacy_history_records, shuttle_massage_binding_operation_aliases, shuttle_massage_binding_operations, shuttle_massage_bookings, shuttle_massage_operations, shuttle_massage_preparations, shuttle_massage_reminders, shuttle_massage_schedule_operations, shuttle_massage_schedule_preparations, shuttle_massage_schedules, shuttle_massage_specialist_bindings, shuttle_massage_specialist_notifications, shuttle_massage_specialist_preference_operations, shuttle_massage_specialist_preferences, shuttle_meal_catalog_operations, shuttle_meal_catalog_preparations, shuttle_meal_catalogs, shuttle_meal_draft_operations, shuttle_meal_drafts, shuttle_miniapp_assignment_actions, shuttle_miniapp_avatar_resolution_actions, shuttle_miniapp_details_actions, shuttle_miniapp_meal_draft_actions, shuttle_miniapp_order_evidence_actions, shuttle_miniapp_orders_selection_actions, shuttle_miniapp_pair_assignment_actions, shuttle_miniapp_profile_actions, shuttle_miniapp_sessions, shuttle_native_payment_evidence_actions, shuttle_notification_resolutions, shuttle_order_allocations, shuttle_order_evidence, shuttle_order_evidence_notices, shuttle_order_evidence_operations, shuttle_order_evidence_preparations, shuttle_order_evidence_reviews, shuttle_order_operations, shuttle_order_preparations, shuttle_order_review_calls, shuttle_order_review_workspaces, shuttle_orders, shuttle_orders_catalog_operations, shuttle_orders_catalog_preparations, shuttle_orders_catalogs, shuttle_orders_selection_operations, shuttle_orders_selections, shuttle_outbox, shuttle_pair_assignment_aliases, shuttle_pair_invitations, shuttle_pair_notifications, shuttle_pair_operations, shuttle_pair_preparations, shuttle_pairs, shuttle_pass_deposit_policy, shuttle_pass_tiers, shuttle_passes, shuttle_payment_evidence, shuttle_payment_notifications, shuttle_payment_operations, shuttle_payment_preparations, shuttle_payment_review_calls, shuttle_payment_review_workspaces, shuttle_payment_reviews, shuttle_payment_versions, shuttle_profile_name_sync, shuttle_profile_operations, shuttle_registration_details, shuttle_registration_details_operations, shuttle_registration_details_request_operations, shuttle_registration_details_requests, shuttle_registration_operations, shuttle_registrations, shuttle_run_event_context, shuttle_settings, shuttle_settings_audit, shuttle_settings_change_proposals, shuttle_task_closure_proposals, shuttle_task_resolutions, shuttle_tasks, shuttle_telegram_prelinks, shuttle_user_event_context, shuttle_user_profiles, shuttle_user_revocation_proposals IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$
DECLARE item record; occupied boolean;
BEGIN
 FOR item IN SELECT tablename FROM pg_catalog.pg_tables
  WHERE schemaname=current_schema() AND starts_with(tablename,'shuttle_') AND tablename<>'shuttle_settings'
 LOOP
  EXECUTE format('SELECT EXISTS(SELECT 1 FROM %I.%I)',current_schema(),item.tablename) INTO occupied;
  IF occupied THEN RAISE EXCEPTION 'baseline data must be exported before rollback'; END IF;
 END LOOP;
 IF NOT (SELECT count(*)=1 AND bool_and(id AND version=1 AND document=
  '{"default_language":"en","max_parallel_tasks":8,"question_timeout_seconds":3600,"auth_cache_ttl_seconds":0}'::jsonb)
  FROM shuttle_settings) THEN
  RAISE EXCEPTION 'baseline settings must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP VIEW shuttle_legacy_history_previews, shuttle_knowledge_notice_candidates;
DROP TABLE shuttle_active_pair_members, shuttle_assignment_aliases, shuttle_assignment_preparations, shuttle_authority_operations, shuttle_authority_preparations, shuttle_avatar_deliveries, shuttle_avatar_delivery_resolutions, shuttle_avatar_draft_operations, shuttle_avatar_drafts, shuttle_avatar_resolution_aliases, shuttle_avatars, shuttle_conversations, shuttle_deployment, shuttle_event_authorities, shuttle_event_change_proposals, shuttle_event_context_operations, shuttle_event_deposit_policy, shuttle_event_operations, shuttle_event_registration_rules, shuttle_events, shuttle_files, shuttle_history_account_receipts, shuttle_iam_name_origins, shuttle_identity_audit, shuttle_inbox, shuttle_knowledge_articles, shuttle_knowledge_moderators, shuttle_knowledge_suggestions, shuttle_users, shuttle_knowledge_notices, shuttle_knowledge_operations, shuttle_knowledge_preparations, shuttle_knowledge_revisions, shuttle_legacy_archive_chunks, shuttle_legacy_archive_operations, shuttle_legacy_archive_preparations, shuttle_legacy_archives, shuttle_legacy_history_applications, shuttle_legacy_history_memberships, shuttle_legacy_history_operations, shuttle_legacy_history_preparations, shuttle_legacy_history_records, shuttle_massage_binding_operation_aliases, shuttle_massage_binding_operations, shuttle_massage_bookings, shuttle_massage_operations, shuttle_massage_preparations, shuttle_massage_reminders, shuttle_massage_schedule_operations, shuttle_massage_schedule_preparations, shuttle_massage_schedules, shuttle_massage_specialist_bindings, shuttle_massage_specialist_notifications, shuttle_massage_specialist_preference_operations, shuttle_massage_specialist_preferences, shuttle_meal_catalog_operations, shuttle_meal_catalog_preparations, shuttle_meal_catalogs, shuttle_meal_draft_operations, shuttle_meal_drafts, shuttle_miniapp_assignment_actions, shuttle_miniapp_avatar_resolution_actions, shuttle_miniapp_details_actions, shuttle_miniapp_meal_draft_actions, shuttle_miniapp_order_evidence_actions, shuttle_miniapp_orders_selection_actions, shuttle_miniapp_pair_assignment_actions, shuttle_miniapp_profile_actions, shuttle_miniapp_sessions, shuttle_native_payment_evidence_actions, shuttle_notification_resolutions, shuttle_order_allocations, shuttle_order_evidence, shuttle_order_evidence_notices, shuttle_order_evidence_operations, shuttle_order_evidence_preparations, shuttle_order_evidence_reviews, shuttle_order_operations, shuttle_order_preparations, shuttle_order_review_calls, shuttle_order_review_workspaces, shuttle_orders, shuttle_orders_catalog_operations, shuttle_orders_catalog_preparations, shuttle_orders_catalogs, shuttle_orders_selection_operations, shuttle_orders_selections, shuttle_outbox, shuttle_pair_assignment_aliases, shuttle_pair_invitations, shuttle_pair_notifications, shuttle_pair_operations, shuttle_pair_preparations, shuttle_pairs, shuttle_pass_deposit_policy, shuttle_pass_tiers, shuttle_passes, shuttle_payment_evidence, shuttle_payment_notifications, shuttle_payment_operations, shuttle_payment_preparations, shuttle_payment_review_calls, shuttle_payment_review_workspaces, shuttle_payment_reviews, shuttle_payment_versions, shuttle_profile_name_sync, shuttle_profile_operations, shuttle_registration_details, shuttle_registration_details_operations, shuttle_registration_details_request_operations, shuttle_registration_details_requests, shuttle_registration_operations, shuttle_registrations, shuttle_run_event_context, shuttle_settings, shuttle_settings_audit, shuttle_settings_change_proposals, shuttle_task_closure_proposals, shuttle_task_resolutions, shuttle_tasks, shuttle_telegram_prelinks, shuttle_user_event_context, shuttle_user_profiles, shuttle_user_revocation_proposals;
DROP FUNCTION shuttle_guard_assignment_namespace(), shuttle_guard_pair_assignment_namespace(), shuttle_clear_stale_latin_name();
