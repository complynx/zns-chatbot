-- Durable provenance remains after the removable offline importer is retired.
CREATE TABLE core.legacy_order_import_references (
  source_key text PRIMARY KEY CHECK(source_key ~ '^[0-9a-f]{64}$'),
  bot_id bigint NOT NULL CHECK(bot_id>0),
  event_id text NOT NULL REFERENCES core.order_events(id),
  source_domain text NOT NULL CHECK(source_domain IN ('configuration','orders','order_capacity','proof')),
  source_record_sha256 text NOT NULL CHECK(source_record_sha256 ~ '^[0-9a-f]{64}$'),
  target_id text NOT NULL,
  source_record jsonb NOT NULL,
  UNIQUE(bot_id,source_domain,target_id)
);
