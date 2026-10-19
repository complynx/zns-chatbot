-- +goose Up
-- Orders retain the exact approved result; current state is tracked separately.
CREATE TABLE shuttle_order_preparations (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 source_draft_version bigint NOT NULL CHECK(source_draft_version>0),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 arguments jsonb NOT NULL CHECK(jsonb_typeof(arguments)='object' AND octet_length(arguments::text)<=65536),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object' AND octet_length(snapshot::text)<=65536),
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 consumed_at timestamptz
);
CREATE INDEX shuttle_order_preparations_pending
 ON shuttle_order_preparations(user_id,event_id,source_draft_version,expires_at DESC)
 WHERE consumed_at IS NULL;

CREATE TABLE shuttle_orders (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 source_draft_version bigint NOT NULL CHECK(source_draft_version>0),
 preparation_id uuid NOT NULL UNIQUE REFERENCES shuttle_order_preparations(id),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 version bigint NOT NULL CHECK(version>0),
 state text NOT NULL CHECK(state IN ('submitted','cancelled')),
 submitted_result jsonb NOT NULL CHECK(jsonb_typeof(submitted_result)='object' AND octet_length(submitted_result::text)<=65536),
 sales_closes_at timestamptz NOT NULL,
 submitted_at timestamptz NOT NULL,
 cancelled_at timestamptz,
 UNIQUE(user_id,event_id,source_draft_version),
 CHECK(sales_closes_at>submitted_at),
 CHECK((state='submitted' AND cancelled_at IS NULL) OR (state='cancelled' AND cancelled_at IS NOT NULL))
);
CREATE INDEX shuttle_orders_owner_event ON shuttle_orders(user_id,event_id,id);

-- Prepare and effect receipts are separate phases of the same transport operation.
CREATE TABLE shuttle_order_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('prepare','submit','cancel')),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 preparation_id uuid REFERENCES shuttle_order_preparations(id),
 order_id uuid REFERENCES shuttle_orders(id),
 business_hash bytea NOT NULL CHECK(octet_length(business_hash)=32),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL CHECK(jsonb_typeof(result)='object' AND octet_length(result::text)<=65536),
 source text NOT NULL CHECK(source IN ('agent','miniapp','telegram')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id,kind),
 CHECK((kind='prepare' AND preparation_id IS NOT NULL AND order_id IS NULL)
    OR (kind='submit' AND preparation_id IS NOT NULL AND order_id IS NOT NULL)
    OR (kind='cancel' AND preparation_id IS NULL AND order_id IS NOT NULL))
);
CREATE INDEX shuttle_order_operations_recent
 ON shuttle_order_operations(user_id,event_id,created_at DESC,operation_id,kind);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_order_operations,shuttle_orders,shuttle_order_preparations IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_order_operations)
 OR EXISTS(SELECT 1 FROM shuttle_orders)
 OR EXISTS(SELECT 1 FROM shuttle_order_preparations) THEN
  RAISE EXCEPTION 'Order snapshots, preparations and operation history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_order_operations;
DROP TABLE shuttle_orders;
DROP TABLE shuttle_order_preparations;
