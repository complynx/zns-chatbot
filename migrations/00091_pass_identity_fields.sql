-- +goose Up
CREATE TABLE shuttle_registration_identity_fields (
    registration_id uuid PRIMARY KEY REFERENCES shuttle_registrations(id) ON DELETE CASCADE,
    legal_name boolean NOT NULL,
    passport_number boolean NOT NULL,
    passport_country boolean NOT NULL,
    passport_photo boolean NOT NULL
);

-- A completed request for the same owner and unchanged event version is
-- evidence of the fields requested before that owner joined the event.
INSERT INTO shuttle_registration_identity_fields
    (registration_id, legal_name, passport_number, passport_country, passport_photo)
SELECT DISTINCT ON (r.id) r.id, d.legal_name_required, d.passport_number_required,
       cardinality(d.passport_countries) > 0, d.passport_photo_required
FROM shuttle_registrations r
JOIN shuttle_events e ON e.id = r.event_id
JOIN shuttle_registration_details_requests d
  ON d.event_id = r.event_id AND d.event_version = e.version AND d.user_id = r.user_id
WHERE d.completed_at <= r.requested_at
ORDER BY r.id, d.completed_at DESC, d.id;

-- +goose Down
LOCK TABLE shuttle_registration_identity_fields IN ACCESS EXCLUSIVE MODE;
-- +goose StatementBegin
DO $$ BEGIN
    IF EXISTS (SELECT FROM shuttle_registration_identity_fields) THEN
        RAISE EXCEPTION 'preserve registration identity field history before downgrade';
    END IF;
END $$;
-- +goose StatementEnd
DROP TABLE shuttle_registration_identity_fields;
