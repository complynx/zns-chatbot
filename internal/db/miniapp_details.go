package db

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
)

var ErrDetailsRequestUnavailable = errors.New("registration details request unavailable")

// MiniAppDetailsForm reads the owner's event requirements under the current session.
// Only the event's country category leaves the protected store.
func (s *Store) MiniAppDetailsForm(ctx context.Context, botID int64, bearer, generation [sha256.Size]byte,
	id uuid.UUID, protector *registrationdetails.Protector) (DetailsRequestStatus, string, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return DetailsRequestStatus{}, "", err
	}
	defer rollbackLegacyArchive(ctx, tx)
	session, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return DetailsRequestStatus{}, "", err
	}
	q := s.q.WithTx(tx)
	owner := session.User.ID
	row, err := q.GetRegistrationDetailsRequest(ctx, sqlc.GetRegistrationDetailsRequestParams{ID: id, UserID: owner})
	if err != nil {
		return DetailsRequestStatus{}, "", err
	}
	status, err := registrationDetailsStatus(ctx, q, owner)
	if err != nil {
		return DetailsRequestStatus{}, "", err
	}
	category := ""
	if len(row.PassportCountries) > 0 && status.PassportCountryPresent {
		record, _, readErr := readRegistrationDetails(ctx, q, owner, protector)
		if readErr != nil {
			return DetailsRequestStatus{}, "", readErr
		}
		if slices.Contains(row.PassportCountries, record.PassportCountry) {
			category = record.PassportCountry
		} else {
			category = "foreign"
		}
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return DetailsRequestStatus{}, "", err
	}
	return detailsRequestStatus(row, status, now.Time), category, nil
}

// MutateMiniAppRegistrationDetails validates the session and saves the private
// field, redacted receipt, expectations, and manual action in one transaction.
// IAM must be checked before calling it. This operation never locks an event.
func (s *Store) MutateMiniAppRegistrationDetails(
	ctx context.Context, botID int64, bearer, generation [sha256.Size]byte,
	mutation registrationdetails.Mutation, detailsRequest *uuid.UUID, protector *registrationdetails.Protector,
) (registrationdetails.Status, error) {
	if detailsRequest != nil && *detailsRequest == uuid.Nil {
		return registrationdetails.Status{}, registrationdetails.ErrInvalid
	}
	m, err := mutation.Normalize()
	if err != nil {
		return registrationdetails.Status{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	attempt, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	// Omitting the request preserves the canonical digest of existing unbound operations.
	request, err := json.Marshal(struct {
		registrationdetails.Mutation

		DetailsRequest *uuid.UUID `json:"details_request,omitempty"`
	}{m, detailsRequest})
	if err != nil {
		return registrationdetails.Status{}, err
	}
	digest, err := protector.Fingerprint(attempt.User.ID, request)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	status, replayed, err := mutateRegistrationDetails(ctx, tx, attempt.User.ID, m, digest, protector)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	if detailsRequest != nil && !replayed {
		if err = grantSubmittedDetailsField(ctx, tx, attempt.User, *detailsRequest, m.Field); err != nil {
			return registrationdetails.Status{}, err
		}
	}
	_, err = tx.Exec(ctx, `INSERT INTO shuttle_miniapp_details_actions(user_id,operation_id,field)
VALUES($1,$2,$3) ON CONFLICT(user_id,operation_id) DO NOTHING`, attempt.User.ID, m.OperationID, m.Field)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	return status, tx.Commit(ctx)
}

// The request records the event's normalized fields at its version. Check that
// version without taking an event lock after the owner/session locks. A form
// submitted before registration still saves the owner's private value.
func grantSubmittedDetailsField(ctx context.Context, tx pgx.Tx, owner User, request uuid.UUID,
	field registrationdetails.Field,
) error {
	var valid bool
	err := tx.QueryRow(ctx, `WITH requested AS (
 SELECT d.event_id FROM shuttle_registration_details_requests d JOIN shuttle_events e ON e.id=d.event_id
 WHERE d.id=$1 AND d.user_id=$2 AND d.superseded_at IS NULL AND d.expires_at>clock_timestamp()
 AND e.bot_id=$3 AND e.organization_id=$4 AND e.version=d.event_version AND 'registrations'=ANY(e.modules)
 AND CASE $5::text WHEN 'legal_name' THEN d.legal_name_required WHEN 'passport_number' THEN d.passport_number_required
  WHEN 'passport_country' THEN cardinality(d.passport_countries)>0 WHEN 'passport_photo' THEN d.passport_photo_required
  ELSE false END
), granted AS (
 INSERT INTO shuttle_registration_identity_fields AS f
 (registration_id,legal_name,passport_number,passport_country,passport_photo)
 SELECT r.id,$5='legal_name',$5='passport_number',$5='passport_country',$5='passport_photo'
 FROM requested JOIN shuttle_registrations r ON r.event_id=requested.event_id AND r.user_id=$2
 WHERE r.state IN ('waitlisted','assigned')
 ON CONFLICT(registration_id) DO UPDATE SET legal_name=f.legal_name OR EXCLUDED.legal_name,
 passport_number=f.passport_number OR EXCLUDED.passport_number,
 passport_country=f.passport_country OR EXCLUDED.passport_country,
 passport_photo=f.passport_photo OR EXCLUDED.passport_photo
) SELECT EXISTS(SELECT FROM requested)`, request, owner.ID, owner.BotID, owner.Organization, field).Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return ErrDetailsRequestUnavailable
	}
	return nil
}

// MiniAppRegistrationDetails returns only presence/version, or an immutable
// historical receipt when operation is set. The owner comes from the session.
func (s *Store) MiniAppRegistrationDetails(
	ctx context.Context, botID int64, bearer, generation [sha256.Size]byte, operation uuid.UUID,
) (registrationdetails.Status, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	attempt, err := s.lockMiniAppBearer(ctx, tx, botID, bearer, generation)
	if err != nil {
		return registrationdetails.Status{}, err
	}
	if operation == uuid.Nil {
		return registrationDetailsStatus(ctx, s.q.WithTx(tx), attempt.User.ID)
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT result FROM shuttle_registration_details_operations WHERE user_id=$1 AND "+
		"operation_id=$2", attempt.User.ID, operation).
		Scan(&raw); err != nil {
		return registrationdetails.Status{}, err
	}
	var status registrationdetails.Status
	err = json.Unmarshal(raw, &status)
	return status, err
}
