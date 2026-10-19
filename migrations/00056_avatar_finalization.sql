-- +goose Up
ALTER TABLE shuttle_avatars
 ALTER COLUMN task_id DROP NOT NULL,
 ALTER COLUMN delivery_id DROP NOT NULL,
 ALTER COLUMN source_file_id DROP NOT NULL,
 ADD COLUMN draft_id uuid,
 ADD COLUMN draft_version bigint,
 ADD COLUMN native boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT shuttle_avatars_id_owner UNIQUE(id,user_id),
 ADD CONSTRAINT shuttle_avatars_draft_owner FOREIGN KEY(draft_id,user_id) REFERENCES shuttle_avatar_drafts(id,user_id),
 ADD CONSTRAINT shuttle_avatars_origin CHECK (
  (task_id IS NOT NULL AND delivery_id IS NOT NULL AND source_file_id IS NOT NULL
   AND draft_id IS NULL AND draft_version IS NULL AND NOT native)
  OR
  (task_id IS NULL AND delivery_id IS NULL AND source_file_id IS NULL
   AND draft_id IS NOT NULL AND draft_version IS NOT NULL AND draft_version>0)
 );
CREATE TABLE shuttle_avatar_deliveries (
 id uuid PRIMARY KEY,
 artifact_id uuid NOT NULL UNIQUE,
 user_id uuid NOT NULL,
 telegram_user_id bigint NOT NULL CHECK(telegram_user_id>0),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN
 ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked','dismissed')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 attempt_id uuid,
 receipt jsonb NOT NULL DEFAULT '{}' CHECK(jsonb_typeof(receipt)='object' AND octet_length(receipt::text)<=4096),
 next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(artifact_id,user_id) REFERENCES shuttle_avatars(id,user_id),
 CHECK(state<>'send_inflight' OR attempt_id IS NOT NULL)
);
CREATE INDEX shuttle_avatar_deliveries_ready ON shuttle_avatar_deliveries(next_attempt_at,id)
 WHERE state IN ('pending','retryable');

-- +goose Down
-- Keep artifact bytes and delivery evidence until deliberate exported removal.
LOCK TABLE shuttle_avatars,shuttle_avatar_deliveries IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatars WHERE draft_id IS NOT NULL)
 OR EXISTS(SELECT 1 FROM shuttle_avatar_deliveries) THEN
  RAISE EXCEPTION 'export finalized avatar bytes and delivery history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_avatar_deliveries;
ALTER TABLE shuttle_avatars
 DROP CONSTRAINT shuttle_avatars_origin,
 DROP CONSTRAINT shuttle_avatars_draft_owner,
 DROP CONSTRAINT shuttle_avatars_id_owner,
 DROP COLUMN draft_id,
 DROP COLUMN draft_version,
 DROP COLUMN native,
 ALTER COLUMN task_id SET NOT NULL,
 ALTER COLUMN delivery_id SET NOT NULL,
 ALTER COLUMN source_file_id SET NOT NULL;
