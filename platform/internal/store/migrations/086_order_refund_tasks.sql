CREATE TABLE core.order_refund_tasks (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 order_id text NOT NULL REFERENCES core.orders(id),
 event_id text NOT NULL REFERENCES core.order_events(id),
 owner text NOT NULL REFERENCES core.users(id),
 displacement_version bigint NOT NULL CHECK(displacement_version>0),
 payment_attempt text NOT NULL,
 amount_cents bigint NOT NULL CHECK(amount_cents>0 AND amount_cents<=100000000000),
 extras_cents jsonb NOT NULL CHECK(jsonb_typeof(extras_cents)='object'),
 currency text NOT NULL DEFAULT 'BYN' CHECK(currency='BYN'),
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','refunded')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 ambassador text REFERENCES core.users(id),
 routing_reason text NOT NULL DEFAULT 'ambassador_missing' CHECK(routing_reason IN ('','ambassador_missing','ambassador_unavailable')),
 notification_id bigint REFERENCES core.order_notifications(id),
 next_route_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 refunded_at timestamptz,
 refunded_by text REFERENCES core.users(id),
 UNIQUE(order_id,displacement_version),
 CHECK((state='pending' AND refunded_at IS NULL AND refunded_by IS NULL) OR
       (state='refunded' AND refunded_at IS NOT NULL AND refunded_by IS NOT NULL))
);
CREATE INDEX order_refund_pending_route ON core.order_refund_tasks(next_route_at,id) WHERE state='pending';
CREATE INDEX order_refund_request_lineage ON core.order_notifications
 ((payload->>'refund_id'),recipient,delivery_state,id) WHERE payload->>'kind'='refund_request';
CREATE TABLE core.order_refund_operations (
 actor text NOT NULL REFERENCES core.users(id),
 key text NOT NULL,
 request_hash text NOT NULL,
 task_id bigint NOT NULL REFERENCES core.order_refund_tasks(id),
 PRIMARY KEY(actor,key)
);
CREATE TABLE core.order_refund_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 task_id bigint NOT NULL REFERENCES core.order_refund_tasks(id),
 actor text NOT NULL REFERENCES core.users(id),
 action text NOT NULL CHECK(action IN ('created','routed','refunded')),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
