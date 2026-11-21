package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/events"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

// EnrolledPassDetailsRequest opens the current form when an existing registration
// lacks a field grant. Collecting details for an issued pass can continue after sales close.
func (s *Store) EnrolledPassDetailsRequest(
	ctx context.Context,
	scope PaymentScope,
	event events.Event,
) (uuid.UUID, error) {
	fields := event.Document.Registration.DetailsRequirements
	if fields == nil {
		return uuid.Nil, nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	q := s.q.WithTx(tx)
	locked, err := q.LockContextEvent(ctx, sqlc.LockContextEventParams{
		ID: event.ID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return uuid.Nil, err
	}
	if locked.Version != event.Version {
		return uuid.Nil, ErrConflict
	}
	var needsGrant bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT FROM shuttle_registrations r
 LEFT JOIN shuttle_registration_identity_fields f ON f.registration_id=r.id
 WHERE r.event_id=$1 AND r.user_id=$2 AND r.state IN ('waitlisted','assigned')
 AND (($3 AND NOT COALESCE(f.legal_name,false)) OR ($4 AND NOT COALESCE(f.passport_number,false))
 OR ($5 AND NOT COALESCE(f.passport_country,false)) OR ($6 AND NOT COALESCE(f.passport_photo,false))))`,
		event.ID, scope.UserID, fields.LegalName, fields.PassportNumber, len(fields.PassportCountries) > 0,
		fields.PassportPhoto).Scan(&needsGrant)
	if err != nil || !needsGrant {
		return uuid.Nil, err
	}
	owner := ProfileScope{
		BotID:        scope.BotID,
		Organization: scope.Organization,
		Subject:      scope.Subject,
		UserID:       scope.UserID,
	}
	if err = lockProfileOwner(ctx, tx, owner, false); err != nil {
		return uuid.Nil, err
	}
	if err = q.LockEventOperation(ctx, "details-requests:"+scope.UserID.String()); err != nil {
		return uuid.Nil, err
	}
	form, err := ensureDetailsRequest(ctx, tx, q, scope.UserID, event, time.Time{})
	if err != nil {
		return uuid.Nil, err
	}
	return form.Request.ID, tx.Commit(ctx)
}

// Fields captured at enrollment bound what the organiser can export after an event edit.
func createEventRegistration(
	ctx context.Context,
	q *sqlc.Queries,
	event events.Event,
	input sqlc.CreateRegistrationParams,
) (sqlc.ShuttleRegistration, error) {
	if err := requireRegistrationDetails(ctx, q, event, input.UserID); err != nil {
		return sqlc.ShuttleRegistration{}, err
	}
	row, err := q.CreateRegistration(ctx, input)
	fields := event.Document.Registration.DetailsRequirements
	if err != nil || fields == nil {
		return row, err
	}
	err = q.RecordRegistrationIdentityFields(ctx, sqlc.RecordRegistrationIdentityFieldsParams{
		RegistrationID: row.ID, LegalName: fields.LegalName, PassportNumber: fields.PassportNumber,
		PassportCountry: len(fields.PassportCountries) > 0, PassportPhoto: fields.PassportPhoto,
	})
	return row, err
}

// Caller holds the event lock and owner's SHARE lock until the effect commits.
// Details writers take that owner's UPDATE lock, so readiness cannot race a write.
func requireRegistrationDetails(ctx context.Context, q *sqlc.Queries, event events.Event, userID uuid.UUID) error {
	requirements := event.Document.Registration.DetailsRequirements
	if requirements == nil {
		return nil
	}
	status, err := registrationDetailsStatus(ctx, q, userID)
	if err != nil {
		return err
	}
	missing := requirements.Missing(status)
	if len(missing) == 0 {
		return nil
	}
	return &registrationdetails.MissingError{MissingFields: missing, DetailsVersion: status.Version}
}
