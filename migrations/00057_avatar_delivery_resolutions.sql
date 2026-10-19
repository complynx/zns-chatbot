-- +goose Up
CREATE TABLE shuttle_avatar_delivery_resolutions (
 bot_id bigint NOT NULL, organization_id text NOT NULL, actor text NOT NULL,
 operation_id uuid NOT NULL, proposal_id uuid NOT NULL UNIQUE,
 delivery_id uuid NOT NULL REFERENCES shuttle_avatar_deliveries(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 expires_at timestamptz NOT NULL,
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object' AND octet_length(document::text)<=16384),
 result jsonb CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=16384),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);

-- +goose Down
LOCK TABLE shuttle_avatar_delivery_resolutions IN ACCESS EXCLUSIVE MODE NOWAIT;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_avatar_delivery_resolutions) THEN
  RAISE EXCEPTION 'export and deliberately remove avatar resolution history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_avatar_delivery_resolutions;
