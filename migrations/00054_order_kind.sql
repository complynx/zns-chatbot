-- +goose Up
-- Existing writers omit order_kind and remain service writers during expansion.
LOCK TABLE shuttle_order_operations,shuttle_order_preparations,shuttle_orders,shuttle_order_evidence IN ACCESS EXCLUSIVE MODE;
ALTER TABLE shuttle_order_preparations ADD COLUMN order_kind text NOT NULL DEFAULT 'service'
 CHECK(order_kind IN ('service','meal'));
ALTER TABLE shuttle_orders ADD COLUMN order_kind text NOT NULL DEFAULT 'service'
 CHECK(order_kind IN ('service','meal'));
ALTER TABLE shuttle_order_operations ADD COLUMN order_kind text NOT NULL DEFAULT 'service'
 CHECK(order_kind IN ('service','meal'));

ALTER TABLE shuttle_order_preparations ADD CONSTRAINT order_preparation_identity
 UNIQUE(id,user_id,event_id,order_kind);
ALTER TABLE shuttle_orders ADD CONSTRAINT order_identity UNIQUE(id,user_id,event_id,order_kind);
ALTER TABLE shuttle_orders ADD CONSTRAINT order_preparation_kind
 FOREIGN KEY(preparation_id,user_id,event_id,order_kind)
 REFERENCES shuttle_order_preparations(id,user_id,event_id,order_kind);
ALTER TABLE shuttle_order_operations ADD CONSTRAINT order_operation_preparation_kind
 FOREIGN KEY(preparation_id,user_id,event_id,order_kind)
 REFERENCES shuttle_order_preparations(id,user_id,event_id,order_kind);
ALTER TABLE shuttle_order_operations ADD CONSTRAINT order_operation_order_kind
 FOREIGN KEY(order_id,user_id,event_id,order_kind)
 REFERENCES shuttle_orders(id,user_id,event_id,order_kind);

-- Evidence remains service-only until meal authority/capacity policy is implemented.
ALTER TABLE shuttle_order_evidence ADD COLUMN order_kind text NOT NULL DEFAULT 'service'
 CHECK(order_kind='service');
ALTER TABLE shuttle_order_evidence ADD CONSTRAINT order_evidence_service_identity
 FOREIGN KEY(order_id,user_id,event_id,order_kind)
 REFERENCES shuttle_orders(id,user_id,event_id,order_kind);

ALTER TABLE shuttle_orders DROP CONSTRAINT shuttle_orders_user_id_event_id_source_draft_version_key;
ALTER TABLE shuttle_orders ADD CONSTRAINT order_canonical_identity
 UNIQUE(user_id,event_id,order_kind,source_draft_version);
DROP INDEX shuttle_order_preparations_pending;
CREATE INDEX shuttle_order_preparations_pending
 ON shuttle_order_preparations(user_id,event_id,order_kind,source_draft_version,expires_at DESC)
 WHERE consumed_at IS NULL;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_order_operations,shuttle_order_preparations,shuttle_orders,shuttle_order_evidence IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_orders WHERE order_kind<>'service')
 OR EXISTS(SELECT 1 FROM shuttle_order_preparations WHERE order_kind<>'service')
 OR EXISTS(SELECT 1 FROM shuttle_order_operations WHERE order_kind<>'service') THEN
  RAISE EXCEPTION 'Meal order identity/history must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_order_evidence DROP CONSTRAINT order_evidence_service_identity;
ALTER TABLE shuttle_order_evidence DROP COLUMN order_kind;
ALTER TABLE shuttle_order_operations DROP CONSTRAINT order_operation_order_kind;
ALTER TABLE shuttle_order_operations DROP CONSTRAINT order_operation_preparation_kind;
ALTER TABLE shuttle_orders DROP CONSTRAINT order_preparation_kind;
ALTER TABLE shuttle_orders DROP CONSTRAINT order_identity;
ALTER TABLE shuttle_order_preparations DROP CONSTRAINT order_preparation_identity;
ALTER TABLE shuttle_orders DROP CONSTRAINT order_canonical_identity;
ALTER TABLE shuttle_orders ADD UNIQUE(user_id,event_id,source_draft_version);
DROP INDEX shuttle_order_preparations_pending;
ALTER TABLE shuttle_order_operations DROP COLUMN order_kind;
ALTER TABLE shuttle_orders DROP COLUMN order_kind;
ALTER TABLE shuttle_order_preparations DROP COLUMN order_kind;
CREATE INDEX shuttle_order_preparations_pending
 ON shuttle_order_preparations(user_id,event_id,source_draft_version,expires_at DESC)
 WHERE consumed_at IS NULL;
