-- +goose Up
-- One immutable decision revision, shared by agent and native transports.
CREATE TABLE shuttle_order_review_workspaces (
 id uuid PRIMARY KEY,
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 evidence_id uuid NOT NULL REFERENCES shuttle_order_evidence(id),
 revision bigint NOT NULL CHECK(revision>0),
 proposal_id uuid NOT NULL UNIQUE REFERENCES shuttle_order_evidence_preparations(id),
 state text NOT NULL CHECK(state IN ('prepared','cancelled','completed')),
 UNIQUE(actor_id,evidence_id,revision)
);
CREATE TABLE shuttle_order_review_calls (
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('save','cancel','commit')),
 source text NOT NULL CHECK(source IN ('agent','miniapp')),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 workspace_id uuid NOT NULL REFERENCES shuttle_order_review_workspaces(id),
 proposal_id uuid,
 expires_at timestamptz,
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object' AND octet_length(document::text)<=65536),
 result jsonb CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=65536),
 completed_at timestamptz,
 PRIMARY KEY(actor_id,operation_id),
 CHECK((result IS NULL)=(completed_at IS NULL)),
 CHECK(kind='commit' OR result IS NOT NULL),
 CHECK((kind='commit' AND proposal_id IS NOT NULL AND expires_at IS NOT NULL) OR
 (kind<>'commit' AND proposal_id IS NULL AND expires_at IS NULL))
);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_order_review_calls,shuttle_order_review_workspaces IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_order_review_calls) OR EXISTS(SELECT 1 FROM shuttle_order_review_workspaces) THEN
  RAISE EXCEPTION 'Export order review workspace and receipt history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_order_review_calls;
DROP TABLE shuttle_order_review_workspaces;
