ALTER TABLE core.order_audit ADD COLUMN snapshot jsonb;
CREATE INDEX order_audit_order_history ON core.order_audit(order_id,id DESC);
