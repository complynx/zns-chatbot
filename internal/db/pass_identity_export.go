package db

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/registrationdetails"
	"github.com/complynx/zns-chatbot/internal/registrations"
)

type PassIdentityPolicy struct {
	EventID      uuid.UUID
	EventVersion int64
	EventName    string
	Requirements registrationdetails.Requirements
}

// PassIdentityRow contains the registration's granted fields that the event still requires.
type PassIdentityRow struct {
	RegistrationID  uuid.UUID
	LegalName       string
	PassportNumber  string
	PassportCountry string
	CountryCategory string
	PassportPhoto   string
}

// passIdentityPolicy reads the current configuration under the event authority lock.
func passIdentityPolicy(
	ctx context.Context, q *sqlc.Queries, scope EventScope, eventID uuid.UUID,
) (PassIdentityPolicy, error) {
	config, err := q.GetEventConfiguration(ctx, sqlc.GetEventConfigurationParams{
		ID: eventID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return PassIdentityPolicy{}, err
	}
	policy := PassIdentityPolicy{
		EventID:      eventID,
		EventVersion: config.ShuttleEvent.Version,
		EventName:    config.ShuttleEvent.Name,
	}
	if !slices.Contains(config.ShuttleEvent.Modules, "registrations") {
		return policy, nil
	}
	event, err := eventRecord(config.ShuttleEvent, config.RegistrationRules, nil)
	if err != nil {
		return PassIdentityPolicy{}, err
	}
	if rules := event.Document.Registration; rules != nil && rules.DetailsRequirements != nil {
		policy.Requirements = *rules.DetailsRequirements
	}
	return policy, nil
}

func (p PassIdentityPolicy) available() bool {
	// Canonical rules require a legal name whenever any passport field is requested.
	return p.Requirements.LegalName
}

type passIdentityFields struct {
	legalName, passportNumber, passportCountry, passportPhoto bool
}

func (fields passIdentityFields) row(
	id uuid.UUID,
	record registrationdetails.Record,
	countries []string,
) PassIdentityRow {
	out := PassIdentityRow{RegistrationID: id}
	if fields.legalName {
		out.LegalName = record.LegalName
	}
	if fields.passportNumber {
		out.PassportNumber = record.PassportNumber
	}
	if fields.passportCountry && record.PassportCountry != "" {
		out.PassportCountry = record.PassportCountry
		out.CountryCategory = "foreign"
		if slices.Contains(countries, record.PassportCountry) {
			out.CountryCategory = record.PassportCountry
		}
	}
	if fields.passportPhoto {
		out.PassportPhoto = record.PassportPhoto
	}
	return out
}

// StreamPassIdentityExport reads roster membership and owner records in one statement
// snapshot. The callbacks build a private artifact. CheckPassIdentityExport must
// succeed after the artifact is complete and before sending any of it to the client.
func (s *Store) StreamPassIdentityExport(
	ctx context.Context,
	scope PaymentScope,
	session *MiniAppPairAssignmentSession,
	eventID uuid.UUID,
	protector *registrationdetails.Protector,
	begin func(PassIdentityPolicy) error,
	visit func(PassIdentityRow) error,
) (PassIdentityPolicy, error) {
	if eventID == uuid.Nil || begin == nil || visit == nil {
		return PassIdentityPolicy{}, registrations.ErrInvalid
	}
	if protector == nil {
		return PassIdentityPolicy{}, registrationdetails.ErrProtection
	}
	tx, q, err := s.beginPassAdminRead(ctx, scope, eventID, session)
	if err != nil {
		return PassIdentityPolicy{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	policy, err := passIdentityPolicy(ctx, q, scope.EventScope, eventID)
	if err != nil {
		return PassIdentityPolicy{}, err
	}
	if !policy.available() {
		return PassIdentityPolicy{}, registrations.ErrUnavailable
	}
	if err = begin(policy); err != nil {
		return PassIdentityPolicy{}, err
	}
	required := policy.Requirements
	rows, err := tx.Query(ctx, `SELECT r.id,r.user_id,allow.name,allow.number,allow.country,allow.photo,
COALESCE(d.version,0),d.encrypted_record,
COALESCE(d.legal_name_present,false),COALESCE(d.passport_number_present,false),
COALESCE(d.passport_country_present,false),COALESCE(d.passport_photo_present,false)
FROM shuttle_registrations r JOIN shuttle_passes p ON p.registration_id=r.id
JOIN shuttle_users u ON u.id=r.user_id AND u.bot_id=$2 AND u.zitadel_org=$3
LEFT JOIN shuttle_registration_identity_fields f ON f.registration_id=r.id
CROSS JOIN LATERAL (SELECT COALESCE(f.legal_name AND $4,false) AS name,
 COALESCE(f.passport_number AND $5,false) AS number,COALESCE(f.passport_country AND $6,false) AS country,
 COALESCE(f.passport_photo AND $7,false) AS photo) AS allow
LEFT JOIN shuttle_registration_details d ON d.user_id=r.user_id
 AND (allow.name OR allow.number OR allow.country OR allow.photo)
WHERE r.event_id=$1 AND r.state='assigned' ORDER BY r.id`, eventID, scope.BotID, scope.Organization,
		required.LegalName, required.PassportNumber, len(required.PassportCountries) > 0, required.PassportPhoto)
	if err != nil {
		return PassIdentityPolicy{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var d sqlc.ShuttleRegistrationDetail
		var fields passIdentityFields
		if err = rows.Scan(&id, &d.UserID, &fields.legalName, &fields.passportNumber, &fields.passportCountry,
			&fields.passportPhoto, &d.Version, &d.EncryptedRecord, &d.LegalNamePresent,
			&d.PassportNumberPresent, &d.PassportCountryPresent, &d.PassportPhotoPresent); err != nil {
			return PassIdentityPolicy{}, err
		}
		var record registrationdetails.Record
		if d.Version > 0 {
			record, _, err = openRegistrationDetails(d.UserID, d, protector)
			if err != nil {
				return PassIdentityPolicy{}, err
			}
		}
		if err = visit(fields.row(id, record, required.PassportCountries)); err != nil {
			return PassIdentityPolicy{}, err
		}
	}
	if err = rows.Err(); err != nil {
		return PassIdentityPolicy{}, err
	}
	if err = authorizePairAssignmentMiniApp(ctx, q, scope, session); err != nil {
		return PassIdentityPolicy{}, err
	}
	return policy, nil
}

// CheckPassIdentityExport rechecks current access and the policy used to build the artifact.
func (s *Store) CheckPassIdentityExport(ctx context.Context, scope PaymentScope, session MiniAppPairAssignmentSession,
	eventID uuid.UUID, exported PassIdentityPolicy, currentGlobalAdmin func(context.Context) (bool, error),
) error {
	if eventID == uuid.Nil || exported.EventID != eventID || currentGlobalAdmin == nil {
		return registrations.ErrInvalid
	}
	tx, q, err := s.beginPairAssignmentRead(ctx, scope, eventID, session)
	if err != nil {
		return err
	}
	defer rollbackLegacyArchive(ctx, tx)
	if err = verifyPassIdentityRelease(ctx, q, scope, exported, currentGlobalAdmin); err != nil {
		return err
	}
	return authorizePairAssignmentMiniApp(ctx, q, scope, &session)
}
func verifyPassIdentityRelease(ctx context.Context, q *sqlc.Queries, scope PaymentScope,
	exported PassIdentityPolicy, currentGlobalAdmin func(context.Context) (bool, error),
) error {
	eventID := exported.EventID
	current, err := passIdentityPolicy(ctx, q, scope.EventScope, eventID)
	if err != nil {
		return err
	}
	if !current.available() {
		return registrations.ErrUnavailable
	}
	before, now := exported.Requirements, current.Requirements
	if current.EventVersion != exported.EventVersion || before.LegalName != now.LegalName ||
		before.PassportNumber != now.PassportNumber || before.PassportPhoto != now.PassportPhoto ||
		!slices.Equal(before.PassportCountries, now.PassportCountries) {
		return ErrConflict
	}
	// The event and actor are locked. Resolve remote authority after any
	// waits, while local grants and the selected policy still cannot change.
	scope.GlobalAdmin, err = currentGlobalAdmin(ctx)
	if err != nil {
		return err
	}
	if !scope.GlobalAdmin {
		if err = checkPairEventAdministrator(ctx, q, scope, eventID); err != nil {
			return err
		}
	}
	return nil
}
