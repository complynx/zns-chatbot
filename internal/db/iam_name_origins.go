package db

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// RecordIAMNameOrigin commits before a technical create or after a verified
// existing-account lookup. Reconciliation never downgrades technical/unknown
// provenance to an existing user-supplied name.
func (s *Store) RecordIAMNameOrigin(
	ctx context.Context,
	userID uuid.UUID,
	botID int64,
	organization string,
	technical bool,
) error {
	if userID == uuid.Nil || botID <= 0 || organization == "" {
		return ErrInvalidProfile
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer rollbackLegacyArchive(ctx, tx)
	var subject string
	err = tx.QueryRow(ctx, `SELECT zitadel_subject FROM shuttle_users WHERE id=$1 AND bot_id=$2
 AND (zitadel_org='' OR zitadel_org=$3) AND NOT revoked FOR UPDATE`,
		userID, botID, organization).Scan(&subject)
	if err != nil {
		return err
	}
	if subject != "" {
		if technical {
			if err = verifyRecordedIAMCreate(ctx, tx, userID, subject); err != nil {
				return err
			}
		}
		// Another entry point completed the binding during the lookup. Preserve
		// its provenance; an absent row remains untrusted rather than inferred.
		return tx.Commit(ctx)
	}
	origin := "existing"
	if technical {
		origin = "technical"
	}
	_, err = tx.Exec(
		ctx,
		"INSERT INTO shuttle_iam_name_origins(user_id,origin) VALUES($1,$2)\n"+
			"ON CONFLICT(user_id) DO UPDATE SET origin=CASE WHEN EXCLUDED.origin='technical' AND "+
			"shuttle_iam_name_origins.origin='existing'\n"+
			"THEN 'technical' ELSE shuttle_iam_name_origins.origin END",
		userID,
		origin,
	)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func verifyRecordedIAMCreate(ctx context.Context, tx pgx.Tx, userID uuid.UUID, subject string) error {
	if subject != userID.String() {
		return ErrConflict
	}
	var recorded bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM shuttle_iam_name_origins
WHERE user_id=$1 AND origin='technical')`, userID).Scan(&recorded)
	if err != nil {
		return err
	}
	if !recorded {
		return ErrConflict
	}
	return nil
}

// ImportIAMProfileName can reuse populated names for a newly linked existing
// IAM identity. Prior or uncertain technical provisioning is never auto-imported.
// The read callback runs without a database connection or owner lock. A second
// owner/provenance check prevents overwriting an intervening local change.
func (s *Store) ImportIAMProfileName(
	ctx context.Context, scope ProfileScope, read func(context.Context) (string, string, error),
) (UserProfile, error) {
	if read == nil {
		return UserProfile{}, ErrInvalidProfile
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserProfile{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	p, allowed, err := iamNameImportState(ctx, tx, scope, false)
	if err != nil {
		return p, err
	}
	if err = tx.Commit(ctx); err != nil {
		return p, err
	}
	if !allowed {
		return p, nil
	}
	first, last, err := read(ctx)
	if err != nil {
		return p, err
	}
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)
	return s.importIAMProfileName(ctx, scope, p.Version, first, last)
}

func iamNameImportState(ctx context.Context, tx pgx.Tx, scope ProfileScope, write bool) (UserProfile, bool, error) {
	if err := lockProfileOwner(ctx, tx, scope, write); err != nil {
		return UserProfile{}, false, err
	}
	p, err := readProfile(ctx, tx, scope.UserID)
	if err != nil || p.FullName != "" || p.NameSource != "" || p.NameExpectation != nil {
		return p, false, err
	}
	var allowed bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM shuttle_iam_name_origins WHERE user_id=$1 AND "+
		"origin='existing' AND checked_at IS NULL)", scope.UserID).
		Scan(&allowed)
	return p, allowed, err
}

func (s *Store) importIAMProfileName(
	ctx context.Context, scope ProfileScope, version int64, first, last string,
) (UserProfile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserProfile{}, err
	}
	defer rollbackLegacyArchive(ctx, tx)
	p, allowed, err := iamNameImportState(ctx, tx, scope, true)
	if err != nil {
		return p, err
	}
	if !allowed || p.Version != version {
		return p, nil
	}
	_, err = tx.Exec(
		ctx,
		`UPDATE shuttle_iam_name_origins SET checked_at=clock_timestamp() WHERE user_id=$1`,
		scope.UserID,
	)
	if err != nil {
		return p, err
	}
	// Missing or unusable IAM fields leave the profile for explicit onboarding.
	if usableImportedIAMName(first, last) {
		p.FirstName, p.LastName, p.FullName, p.NameSource = first, last, first+" "+last, "iam"
		p.NameExpectation = nil
		p.Version++
		if err = saveProfile(ctx, tx, p); err != nil {
			return p, err
		}
	}
	return p, tx.Commit(ctx)
}

func usableImportedIAMName(first, last string) bool {
	return validProfileNameComponent(first) && validProfileNameComponent(last) &&
		utf8.RuneCountInString(first+" "+last) <= 256 && !strings.EqualFold(first, last) &&
		!strings.EqualFold(first, "Telegram user") && !strings.EqualFold(first, "Telegram")
}
