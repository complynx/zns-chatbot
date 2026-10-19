-- +goose Up
-- Evidence owns its allocations. Receipts retain admission outcomes after review.
CREATE TABLE shuttle_order_evidence (
 id uuid PRIMARY KEY,
 order_id uuid NOT NULL REFERENCES shuttle_orders(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 file_id uuid NOT NULL REFERENCES shuttle_files(id),
 source_unique_id text NOT NULL,
 reported_amount_minor bigint NOT NULL CHECK(reported_amount_minor BETWEEN 1 AND 9000000000000000),
 quoted_amount_minor bigint NOT NULL CHECK(quoted_amount_minor BETWEEN 0 AND 9000000000000000),
 currency text NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 currency_scale integer NOT NULL CHECK(currency_scale BETWEEN 0 AND 6),
 sent_at timestamptz NOT NULL,
 submitted_at timestamptz NOT NULL,
 due_at timestamptz NOT NULL,
 admission_state text NOT NULL CHECK(admission_state IN ('pending','exhausted','needs_decision')),
 reason text NOT NULL CHECK(char_length(reason)<=128),
 state text NOT NULL CHECK(state IN ('pending','exhausted','needs_decision','confirmed','rejected')),
 version bigint NOT NULL DEFAULT 1 CHECK(version IN (1,2)),
 CHECK((state=admission_state AND version=1) OR (state IN ('confirmed','rejected') AND version=2))
);
CREATE UNIQUE INDEX shuttle_order_evidence_active ON shuttle_order_evidence(order_id) WHERE state<>'rejected';
CREATE INDEX shuttle_order_evidence_provenance ON shuttle_order_evidence(user_id,source_unique_id);
CREATE INDEX shuttle_order_evidence_event ON shuttle_order_evidence(event_id,id);
CREATE TABLE shuttle_order_allocations (
 evidence_id uuid NOT NULL REFERENCES shuttle_order_evidence(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 resource_id text NOT NULL CHECK(resource_id ~ '^[a-z][a-z0-9_-]{0,31}$'),
 units bigint NOT NULL CHECK(units BETWEEN 1 AND 1000000000),
 state text NOT NULL CHECK(state IN ('held','occupied','released')),
 PRIMARY KEY(evidence_id,resource_id)
);
CREATE INDEX shuttle_order_allocations_usage ON shuttle_order_allocations(event_id,resource_id) WHERE state IN ('held','occupied');
CREATE TABLE shuttle_order_evidence_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('submit','review')),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=65536),
 PRIMARY KEY(user_id,operation_id,kind)
);
CREATE TABLE shuttle_order_evidence_reviews (
 evidence_id uuid PRIMARY KEY REFERENCES shuttle_order_evidence(id),
 actor_id uuid NOT NULL REFERENCES shuttle_users(id),
 actor_subject text NOT NULL,
 authority_role text NOT NULL CHECK(authority_role IN ('global_administrator','ambassador','administrator')),
 decision text NOT NULL CHECK(decision IN ('confirmed','rejected')),
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 0 AND 9000000000000000),
 reason text NOT NULL CHECK(char_length(reason) BETWEEN 1 AND 512),
 reviewed_at timestamptz NOT NULL
);
CREATE TABLE shuttle_order_evidence_preparations (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object' AND octet_length(document::text)<=65536),
 expires_at timestamptz NOT NULL,
 UNIQUE(user_id,operation_id)
);
-- Business notices are retained before delivery wiring is enabled.
CREATE TABLE shuttle_order_evidence_notices (
 id uuid PRIMARY KEY,
 evidence_id uuid NOT NULL REFERENCES shuttle_order_evidence(id),
 recipient_id uuid NOT NULL REFERENCES shuttle_users(id),
 kind text NOT NULL CHECK(kind IN ('pending','exhausted','needs_decision','confirmed','rejected')),
 document jsonb NOT NULL CHECK(jsonb_typeof(document)='object' AND octet_length(document::text)<=65536),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(evidence_id,recipient_id,kind)
);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_order_evidence_notices,shuttle_order_evidence_preparations,
 shuttle_order_evidence_reviews,shuttle_order_evidence_operations,shuttle_order_allocations,
 shuttle_order_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_order_evidence)
 OR EXISTS(SELECT 1 FROM shuttle_order_evidence_operations)
 OR EXISTS(SELECT 1 FROM shuttle_order_evidence_preparations)
 OR EXISTS(SELECT 1 FROM shuttle_order_evidence_notices) THEN
  RAISE EXCEPTION 'Order evidence, allocations, review and operation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_order_evidence_notices;
DROP TABLE shuttle_order_evidence_preparations;
DROP TABLE shuttle_order_evidence_reviews;
DROP TABLE shuttle_order_evidence_operations;
DROP TABLE shuttle_order_allocations;
DROP TABLE shuttle_order_evidence;
