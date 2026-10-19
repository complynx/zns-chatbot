-- +goose Up
CREATE TABLE shuttle_legacy_archives (
 id uuid PRIMARY KEY,
 bot_id bigint NOT NULL CHECK(bot_id>0 AND bot_id<4503599627370496),
 organization_id text NOT NULL CHECK(organization_id<>''),
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 size_bytes bigint NOT NULL CHECK(size_bytes>0 AND size_bytes<=269484032),
 validation jsonb NOT NULL,
 created_by uuid NOT NULL REFERENCES shuttle_users(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(bot_id,organization_id,sha256)
);

CREATE TABLE shuttle_legacy_archive_chunks (
 archive_id uuid NOT NULL REFERENCES shuttle_legacy_archives(id) DEFERRABLE INITIALLY DEFERRED,
 ordinal integer NOT NULL CHECK(ordinal>=0 AND ordinal<1028),
 content bytea NOT NULL CHECK(octet_length(content)>0 AND octet_length(content)<=262144),
 PRIMARY KEY(archive_id,ordinal)
);

CREATE TABLE shuttle_legacy_archive_preparations (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 arguments jsonb NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '5 minutes',
 UNIQUE(user_id,operation_id)
);

CREATE TABLE shuttle_legacy_archive_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 proposal_id uuid NOT NULL REFERENCES shuttle_legacy_archive_preparations(id),
 archive_id uuid NOT NULL REFERENCES shuttle_legacy_archives(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 receipt jsonb NOT NULL,
 PRIMARY KEY(user_id,operation_id)
);

-- +goose Down
-- Export archival bytes and operation history before intentional removal.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_legacy_archives)
 OR EXISTS(SELECT 1 FROM shuttle_legacy_archive_preparations) THEN
  RAISE EXCEPTION 'legacy archives and preparation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_legacy_archive_operations;
DROP TABLE shuttle_legacy_archive_preparations;
DROP TABLE shuttle_legacy_archive_chunks;
DROP TABLE shuttle_legacy_archives;
