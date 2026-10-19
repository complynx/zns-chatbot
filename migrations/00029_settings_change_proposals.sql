-- +goose Up
CREATE TABLE shuttle_settings_change_proposals (
 bot_id bigint NOT NULL CHECK(bot_id>0),
 organization_id text NOT NULL CHECK(organization_id<>''),
 actor text NOT NULL CHECK(actor<>''),
 operation_id text NOT NULL CHECK(length(operation_id) BETWEEN 1 AND 128),
 proposal_id uuid NOT NULL UNIQUE,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 expires_at timestamptz NOT NULL,
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object'),
 result jsonb CHECK(jsonb_typeof(result)='object'),
 PRIMARY KEY(bot_id,organization_id,actor,operation_id)
);

-- +goose Down
-- Binary rollback retains the proposal and result audit.
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_settings_change_proposals) THEN
  RAISE EXCEPTION 'settings approval and result history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_settings_change_proposals;
