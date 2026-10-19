-- +goose Up
CREATE INDEX shuttle_outbox_unresolved_metrics_idx ON shuttle_outbox(state,created_at)
WHERE state NOT IN ('delivered','delivery_blocked','delivery_failed');

-- +goose Down
DROP INDEX shuttle_outbox_unresolved_metrics_idx;
