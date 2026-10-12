-- +goose Up
CREATE TABLE shuttle_task_resolutions (
 task_id uuid NOT NULL REFERENCES shuttle_tasks(id), operation_id text NOT NULL,
 actor text NOT NULL, expected_version bigint NOT NULL, resulting_version bigint NOT NULL,
 reason text NOT NULL, previous_state text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(task_id,operation_id)
);
-- +goose Down
DROP TABLE shuttle_task_resolutions;
