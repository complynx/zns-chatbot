-- +goose Up
ALTER TABLE shuttle_order_evidence DROP CONSTRAINT shuttle_order_evidence_order_kind_check;
ALTER TABLE shuttle_order_evidence ADD CONSTRAINT shuttle_order_evidence_order_kind_check
 CHECK(order_kind IN ('service','meal'));

-- Full consent is stored once, independently of bounded JSON tool envelopes.
ALTER TABLE shuttle_order_evidence_preparations ADD COLUMN presentation text NOT NULL DEFAULT ''
 CHECK(octet_length(presentation)<=262144);

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_order_evidence IN ACCESS EXCLUSIVE MODE;
 LOCK TABLE shuttle_order_evidence_preparations IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_order_evidence WHERE order_kind='meal') THEN
  RAISE EXCEPTION 'Meal payment evidence must be exported before rollback';
 END IF;
 IF EXISTS(SELECT 1 FROM shuttle_order_evidence_preparations WHERE presentation<>'') THEN
  RAISE EXCEPTION 'Order review presentations must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_order_evidence DROP CONSTRAINT shuttle_order_evidence_order_kind_check;
ALTER TABLE shuttle_order_evidence ADD CONSTRAINT shuttle_order_evidence_order_kind_check
 CHECK(order_kind='service');

ALTER TABLE shuttle_order_evidence_preparations DROP COLUMN presentation;
