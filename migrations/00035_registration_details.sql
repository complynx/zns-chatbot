-- +goose Up
CREATE TABLE shuttle_registration_details (
 user_id uuid PRIMARY KEY REFERENCES shuttle_users(id),
 version bigint NOT NULL CHECK(version > 0),
 encrypted_record bytea NOT NULL CHECK(octet_length(encrypted_record) >= 29),
 legal_name_present boolean NOT NULL,
 passport_number_present boolean NOT NULL
);
CREATE TABLE shuttle_registration_details_operations (
 user_id uuid NOT NULL REFERENCES shuttle_users(id),
 operation_id uuid NOT NULL,
 request_hash bytea NOT NULL CHECK(octet_length(request_hash)=32),
 result jsonb NOT NULL,
 PRIMARY KEY(user_id,operation_id)
);
-- +goose Down
-- Export legal details and immutable receipts with their deployment key before rollback.
-- +goose StatementBegin
DO $$ BEGIN
 LOCK TABLE shuttle_registration_details_operations, shuttle_registration_details IN ACCESS EXCLUSIVE MODE;
 IF EXISTS(SELECT 1 FROM shuttle_registration_details) OR EXISTS(SELECT 1 FROM shuttle_registration_details_operations) THEN
  RAISE EXCEPTION 'registration details and receipts must be exported before rollback';
 END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_registration_details_operations;
DROP TABLE shuttle_registration_details;
