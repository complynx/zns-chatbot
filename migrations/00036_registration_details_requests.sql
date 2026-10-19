-- +goose Up
CREATE TABLE shuttle_registration_details_requests (
 id uuid PRIMARY KEY,
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 event_id uuid NOT NULL REFERENCES shuttle_events(id),
 event_version bigint NOT NULL CHECK(event_version>0),
 event_timezone text NOT NULL,
 legal_name_required boolean NOT NULL,
 passport_number_required boolean NOT NULL,
 created_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL CHECK(expires_at>created_at),
 completed_at timestamptz,
 completed_details_version bigint CHECK(completed_details_version>=0),
 superseded_at timestamptz,
 CHECK(NOT passport_number_required OR legal_name_required),
 CHECK((completed_at IS NULL)=(completed_details_version IS NULL)),
 CHECK(completed_at IS NULL OR superseded_at IS NULL)
);
CREATE UNIQUE INDEX shuttle_details_request_pending ON shuttle_registration_details_requests(user_id,event_id)
 WHERE completed_at IS NULL AND superseded_at IS NULL;
CREATE INDEX shuttle_details_requests_owner ON shuttle_registration_details_requests(user_id,created_at DESC,id);
CREATE TABLE shuttle_registration_details_request_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL,
 PRIMARY KEY(user_id,operation_id)
);
-- +goose Down
-- Preserve request history and immutable operation receipts before rollback.
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_registration_details_request_operations, shuttle_registration_details_requests IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_registration_details_requests) OR EXISTS(SELECT 1 FROM shuttle_registration_details_request_operations) THEN
  RAISE EXCEPTION 'registration details requests and receipts must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_registration_details_request_operations;
DROP TABLE shuttle_registration_details_requests;
