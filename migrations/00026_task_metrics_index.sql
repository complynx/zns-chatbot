-- +goose Up
CREATE INDEX shuttle_tasks_unresolved_metrics_idx ON shuttle_tasks(state,created_at)
WHERE state NOT IN ('completed','failed','cancelled','delivery_failed','closed');

-- +goose Down
DROP INDEX shuttle_tasks_unresolved_metrics_idx;
