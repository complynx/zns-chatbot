-- +goose Up
CREATE TABLE shuttle_legacy_history_records (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL,
 organization_id text NOT NULL,
 source_instance text NOT NULL CHECK(source_instance ~ '^[A-Za-z0-9_.:-]{1,128}$'),
 collection_name text NOT NULL CHECK(collection_name ~ '^[A-Za-z0-9_.:-]{1,128}$'),
 source_key text NOT NULL CHECK(octet_length(source_key) BETWEEN 1 AND 1538),
 source_role text NOT NULL CHECK(source_role IN ('users','messages','passes','files')),
 owner_telegram_id bigint NOT NULL CHECK(owner_telegram_id>0 AND owner_telegram_id<4503599627370496),
 canonical_hash bytea NOT NULL CHECK(octet_length(canonical_hash)=32),
 projection_version text NOT NULL,
 projection jsonb,
 first_archive_id uuid NOT NULL REFERENCES shuttle_legacy_archives(id),
 first_line integer NOT NULL CHECK(first_line>0),
 UNIQUE(bot_id,organization_id,source_instance,collection_name,source_key)
);
CREATE INDEX shuttle_legacy_history_owner ON shuttle_legacy_history_records(bot_id,organization_id,owner_telegram_id,id);

CREATE TABLE shuttle_legacy_history_memberships (
 archive_id uuid NOT NULL REFERENCES shuttle_legacy_archives(id),
 record_id uuid NOT NULL REFERENCES shuttle_legacy_history_records(id),
 source_role text NOT NULL CHECK(source_role IN ('users','messages','passes','files')),
 source_line integer NOT NULL CHECK(source_line>0),
 PRIMARY KEY(archive_id,record_id),
 UNIQUE(archive_id,source_role,source_line)
);

CREATE TABLE shuttle_legacy_history_preparations (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 arguments jsonb NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '5 minutes',
 UNIQUE(user_id,operation_id)
);

CREATE TABLE shuttle_legacy_history_applications (
 archive_id uuid PRIMARY KEY REFERENCES shuttle_legacy_archives(id),
 projection_version text NOT NULL,
 receipt jsonb NOT NULL
);

CREATE TABLE shuttle_legacy_history_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 proposal_id uuid NOT NULL REFERENCES shuttle_legacy_history_preparations(id),
 archive_id uuid NOT NULL REFERENCES shuttle_legacy_history_applications(archive_id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 receipt jsonb NOT NULL,
 PRIMARY KEY(user_id,operation_id)
);

CREATE VIEW shuttle_legacy_history_previews AS
SELECT id,bot_id,organization_id,owner_telegram_id,source_instance,collection_name,source_key,
source_role,first_archive_id,first_line,projection_version,
CASE WHEN projection->>'kind'='message' THEN jsonb_build_object(
 'kind','message','role',projection->'role','content',left(projection->>'content',256),
 'truncated',length(projection->>'content')>256,
 'recorded_date',CASE WHEN octet_length((projection->'recorded_date')::text)<=512 THEN projection->'recorded_date' ELSE 'null'::jsonb END,
 'recorded_date_omitted',octet_length((projection->'recorded_date')::text)>512,
 'date_policy','preserve_source') ELSE projection END AS preview
FROM shuttle_legacy_history_records WHERE projection IS NOT NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_legacy_history_records)
 OR EXISTS(SELECT 1 FROM shuttle_legacy_history_preparations) THEN
  RAISE EXCEPTION 'historical records and approvals must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP VIEW shuttle_legacy_history_previews;
DROP TABLE shuttle_legacy_history_operations;
DROP TABLE shuttle_legacy_history_applications;
DROP TABLE shuttle_legacy_history_preparations;
DROP TABLE shuttle_legacy_history_memberships;
DROP TABLE shuttle_legacy_history_records;
