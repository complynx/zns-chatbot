-- +goose Up
ALTER TABLE shuttle_files ADD CONSTRAINT shuttle_files_id_owner UNIQUE(id,user_id);
CREATE TABLE shuttle_avatar_drafts (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 source_file_id uuid,
 original bytea NOT NULL CHECK(octet_length(original)>0 AND octet_length(original)<=20971520),
 source_sha256 bytea NOT NULL CHECK(octet_length(source_sha256)=32),
 mime_type text NOT NULL CHECK(mime_type IN ('image/jpeg','image/png')),
 frame_sha256 bytea NOT NULL CHECK(octet_length(frame_sha256)=32),
 transform jsonb NOT NULL CHECK(jsonb_typeof(transform)='object' AND octet_length(transform::text)<=1024),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(id,user_id),
 FOREIGN KEY(source_file_id,user_id) REFERENCES shuttle_files(id,user_id)
);
CREATE INDEX shuttle_avatar_drafts_owner ON shuttle_avatar_drafts(user_id,id);
CREATE TABLE shuttle_avatar_draft_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('create_upload','create_file','save_transform')),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 draft_id uuid NOT NULL,
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=4096),
 native boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id),
 FOREIGN KEY(draft_id,user_id) REFERENCES shuttle_avatar_drafts(id,user_id)
);
CREATE INDEX shuttle_avatar_draft_native_actions ON shuttle_avatar_draft_operations(user_id,created_at DESC) WHERE native;

-- +goose Down
-- Fail closed during concurrent use; hold both table locks through the emptiness check and DROP.
LOCK TABLE shuttle_avatar_draft_operations,shuttle_avatar_drafts IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatar_drafts) OR EXISTS(SELECT 1 FROM shuttle_avatar_draft_operations) THEN
  RAISE EXCEPTION 'export avatar originals and operation history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_avatar_draft_operations;
DROP TABLE shuttle_avatar_drafts;
ALTER TABLE shuttle_files DROP CONSTRAINT shuttle_files_id_owner;
