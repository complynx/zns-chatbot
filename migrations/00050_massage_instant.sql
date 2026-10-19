-- +goose Up
-- Instant self-reservations use the same ledger, with a distinct proposal kind.
ALTER TABLE shuttle_massage_preparations
 DROP CONSTRAINT shuttle_massage_preparations_kind_check,
 DROP CONSTRAINT shuttle_massage_preparations_check,
 ADD CONSTRAINT shuttle_massage_preparations_kind_check CHECK(kind IN ('book','cancel','instant')),
 ADD CONSTRAINT shuttle_massage_preparations_check CHECK(kind IN ('book','instant') OR booking_id IS NOT NULL);
ALTER TABLE shuttle_massage_operations
 DROP CONSTRAINT shuttle_massage_operations_kind_check,
 ADD CONSTRAINT shuttle_massage_operations_kind_check CHECK(kind IN ('book','cancel','instant'));

-- +goose Down
-- Never turn retained staff history into unsupported ordinary booking data.
LOCK TABLE shuttle_massage_preparations, shuttle_massage_operations IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM shuttle_massage_preparations WHERE kind='instant')
 OR EXISTS(SELECT 1 FROM shuttle_massage_operations WHERE kind='instant') THEN
  RAISE EXCEPTION 'Cannot remove instant self-reservation schema while its history is retained';
 END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_massage_preparations
 DROP CONSTRAINT shuttle_massage_preparations_kind_check,
 DROP CONSTRAINT shuttle_massage_preparations_check,
 ADD CONSTRAINT shuttle_massage_preparations_kind_check CHECK(kind IN ('book','cancel')),
 ADD CONSTRAINT shuttle_massage_preparations_check CHECK(kind='book' OR booking_id IS NOT NULL);
ALTER TABLE shuttle_massage_operations
 DROP CONSTRAINT shuttle_massage_operations_kind_check,
 ADD CONSTRAINT shuttle_massage_operations_kind_check CHECK(kind IN ('book','cancel'));
