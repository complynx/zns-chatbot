-- +goose Up
-- Each workspace row is one exact revision; a later row supersedes it.
CREATE TABLE shuttle_payment_review_workspaces (
 id uuid PRIMARY KEY,
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 evidence_id uuid NOT NULL REFERENCES shuttle_payment_evidence(id),
 revision bigint NOT NULL CHECK(revision > 0),
 proposal_id uuid NOT NULL UNIQUE REFERENCES shuttle_payment_preparations(proposal_id),
 state text NOT NULL CHECK(state IN ('prepared','cancelled','completed')),
 UNIQUE(actor_id,evidence_id,revision)
);
CREATE TABLE shuttle_payment_review_calls (
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id text NOT NULL CHECK(char_length(operation_id) BETWEEN 1 AND 128),
 kind text NOT NULL CHECK(kind IN ('prepare','cancel','commit')),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 workspace_id uuid NOT NULL REFERENCES shuttle_payment_review_workspaces(id),
 proposal_id uuid UNIQUE,
 expires_at timestamptz,
 result jsonb,
 source text NOT NULL CHECK(source IN ('agent','miniapp')),
 completed_at timestamptz,
 PRIMARY KEY(actor_id,operation_id),
 CHECK((result IS NULL)=(completed_at IS NULL)),
 CHECK(kind='commit' OR result IS NOT NULL),
 CHECK((kind='commit' AND proposal_id IS NOT NULL AND expires_at IS NOT NULL) OR
 (kind<>'commit' AND proposal_id IS NULL AND expires_at IS NULL))
);
CREATE INDEX shuttle_payment_review_calls_context ON shuttle_payment_review_calls(actor_id,completed_at DESC)
 WHERE source='miniapp' AND completed_at IS NOT NULL;
-- +goose Down
-- Export workspaces and receipts before removing this workflow.
DROP TABLE shuttle_payment_review_calls;
DROP TABLE shuttle_payment_review_workspaces;
