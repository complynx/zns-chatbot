-- +goose Up
CREATE TABLE shuttle_massage_preparations (
 id uuid PRIMARY KEY,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('book','cancel')),
 source text NOT NULL CHECK(source IN ('agent','miniapp','telegram')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 arguments jsonb NOT NULL CHECK(octet_length(arguments::text)<=16384),
 snapshot jsonb NOT NULL CHECK(octet_length(snapshot::text)<=32768),
 booking_id uuid,
 expires_at timestamptz NOT NULL,
 consumed_at timestamptz,
 consumed_result jsonb CHECK(octet_length(consumed_result::text)<=32768),
 UNIQUE(user_id,operation_id),
 CHECK(kind='book' OR booking_id IS NOT NULL),
 CHECK((consumed_at IS NULL AND consumed_result IS NULL) OR
       (consumed_at IS NOT NULL AND consumed_result IS NOT NULL AND booking_id IS NOT NULL))
);
CREATE INDEX shuttle_massage_pending_owner ON shuttle_massage_preparations(event_id,user_id,id)
 WHERE consumed_at IS NULL;
CREATE TABLE shuttle_massage_bookings (
 id uuid PRIMARY KEY,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 preparation_id uuid NOT NULL UNIQUE REFERENCES shuttle_massage_preparations(id),
 window_id text NOT NULL CHECK(window_id ~ '^[a-z][a-z0-9_-]{0,31}$'),
 specialist_id text NOT NULL CHECK(specialist_id ~ '^[a-z][a-z0-9_-]{0,31}$'),
 service_id text NOT NULL CHECK(service_id ~ '^[a-z][a-z0-9_-]{0,31}$'),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 state text NOT NULL DEFAULT 'booked' CHECK(state IN ('booked','cancelled')),
 starts_at timestamptz NOT NULL,
 treatment_ends_at timestamptz NOT NULL,
 occupancy_ends_at timestamptz NOT NULL,
 requires_table boolean NOT NULL,
 currency text NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 currency_scale integer NOT NULL CHECK(currency_scale BETWEEN 0 AND 4),
 price_minor bigint NOT NULL CHECK(price_minor BETWEEN 0 AND 9000000000000000),
 snapshot jsonb NOT NULL CHECK(octet_length(snapshot::text)<=32768),
 source text NOT NULL CHECK(source IN ('agent','miniapp','telegram')),
 booked_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 cancelled_at timestamptz,
 CHECK(starts_at<treatment_ends_at AND treatment_ends_at<=occupancy_ends_at),
 CHECK((state='booked' AND cancelled_at IS NULL) OR
       (state='cancelled' AND cancelled_at IS NOT NULL))
);
ALTER TABLE shuttle_massage_preparations ADD CONSTRAINT shuttle_massage_preparation_booking_fk
 FOREIGN KEY(booking_id) REFERENCES shuttle_massage_bookings(id);
CREATE INDEX shuttle_massage_active_event_time ON shuttle_massage_bookings(event_id,starts_at)
 WHERE state='booked';
CREATE INDEX shuttle_massage_owner_page ON shuttle_massage_bookings(event_id,user_id,id);
CREATE TABLE shuttle_massage_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 kind text NOT NULL CHECK(kind IN ('book','cancel')),
 source text NOT NULL CHECK(source IN ('agent','miniapp','telegram')),
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 preparation_id uuid NOT NULL REFERENCES shuttle_massage_preparations(id),
 booking_id uuid NOT NULL REFERENCES shuttle_massage_bookings(id),
 result jsonb NOT NULL CHECK(octet_length(result::text)<=32768),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(user_id,operation_id)
);
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_massage_bookings)
 OR EXISTS(SELECT 1 FROM shuttle_massage_preparations)
 OR EXISTS(SELECT 1 FROM shuttle_massage_operations) THEN
  RAISE EXCEPTION 'Export massage booking, approval and operation history before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_massage_operations;
ALTER TABLE shuttle_massage_preparations DROP CONSTRAINT shuttle_massage_preparation_booking_fk;
DROP TABLE shuttle_massage_bookings;
DROP TABLE shuttle_massage_preparations;
