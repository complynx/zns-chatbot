-- +goose Up
ALTER TABLE shuttle_registration_details
    ADD COLUMN passport_country_present boolean NOT NULL DEFAULT false,
    ADD COLUMN passport_photo_present boolean NOT NULL DEFAULT false;
ALTER TABLE shuttle_registration_details_requests
    ADD COLUMN passport_countries text[] NOT NULL DEFAULT '{}',
    ADD COLUMN passport_photo_required boolean NOT NULL DEFAULT false;
ALTER TABLE shuttle_miniapp_details_actions DROP CONSTRAINT shuttle_miniapp_details_actions_field_check;
ALTER TABLE shuttle_miniapp_details_actions ADD CONSTRAINT shuttle_miniapp_details_actions_field_check
    CHECK (field IN ('legal_name','passport_number','passport_country','passport_photo'));

-- +goose Down
LOCK TABLE shuttle_registration_details, shuttle_registration_details_requests, shuttle_miniapp_details_actions
    IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT 1 FROM shuttle_registration_details WHERE passport_country_present OR passport_photo_present)
       OR EXISTS (SELECT 1 FROM shuttle_registration_details_requests WHERE cardinality(passport_countries)>0 OR passport_photo_required)
       THEN RAISE EXCEPTION 'export passport data and request history before downgrade'; END IF;
END $$;
-- +goose StatementEnd
ALTER TABLE shuttle_miniapp_details_actions DROP CONSTRAINT shuttle_miniapp_details_actions_field_check;
ALTER TABLE shuttle_miniapp_details_actions ADD CONSTRAINT shuttle_miniapp_details_actions_field_check
    CHECK (field IN ('legal_name','passport_number'));
ALTER TABLE shuttle_registration_details_requests DROP COLUMN passport_countries, DROP COLUMN passport_photo_required;
ALTER TABLE shuttle_registration_details DROP COLUMN passport_country_present, DROP COLUMN passport_photo_present;
