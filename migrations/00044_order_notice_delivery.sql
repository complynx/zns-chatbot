-- +goose Up
-- Preserve business notice IDs/documents while adding a separate delivery lifecycle.
ALTER TABLE shuttle_order_evidence_notices
 ADD COLUMN state text NOT NULL DEFAULT 'pending' CHECK(state IN
 ('pending','retryable','send_inflight','delivered','delivery_unknown','delivery_failed','delivery_blocked','dismissed')),
 ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 ADD COLUMN attempt_id uuid,
 ADD COLUMN payload jsonb,
 ADD COLUMN receipt jsonb NOT NULL DEFAULT '{}',
 ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT clock_timestamp();
CREATE INDEX shuttle_order_evidence_notices_ready ON shuttle_order_evidence_notices(next_attempt_at,id)
 WHERE state IN ('pending','retryable');
-- +goose Down
-- Delivery history is evidence. Never silently discard an attempted send.
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_order_evidence_notices IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_order_evidence_notices WHERE state<>'pending' OR version<>1
 OR attempt_id IS NOT NULL OR payload IS NOT NULL OR receipt<>'{}'::jsonb) THEN
  RAISE EXCEPTION 'Export order notice delivery history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP INDEX shuttle_order_evidence_notices_ready;
ALTER TABLE shuttle_order_evidence_notices
 DROP COLUMN state, DROP COLUMN version, DROP COLUMN attempt_id,
 DROP COLUMN payload, DROP COLUMN receipt, DROP COLUMN next_attempt_at;
