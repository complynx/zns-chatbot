-- Existing rows have no proven enqueue time. Keep their age unknown.
ALTER TABLE core.delivery_queue ADD COLUMN enqueued_at timestamptz;
ALTER TABLE core.delivery_queue ALTER COLUMN enqueued_at SET DEFAULT clock_timestamp();
