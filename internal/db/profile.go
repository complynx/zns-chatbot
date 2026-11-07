package db

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

var ErrInvalidProfile = errors.New("invalid profile change")

const (
	profileLanguageAuto   = "auto"
	profileSetName        = "set_name"
	profileCompleteName   = "complete_name"
	profileSetLanguage    = "set_language"
	profileNameSourceUser = "user"
)

type ProfileScope struct {
	BotID                 int64
	Organization, Subject string
	UserID                uuid.UUID
}

type NameExpectation struct {
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type UserProfile struct {
	UserID          uuid.UUID        `json:"user_id"`
	Version         int64            `json:"version"`
	FullName        string           `json:"full_name"`
	FirstName       string           `json:"first_name,omitempty"`
	LastName        string           `json:"last_name,omitempty"`
	NameSource      string           `json:"name_source,omitempty"`
	BotLanguage     string           `json:"bot_language"`
	NameExpectation *NameExpectation `json:"name_expectation,omitempty"`
	ObservedAt      time.Time        `json:"observed_at"`
	FirstNameLatin  string           `json:"first_name_latin,omitempty"`
	LastNameLatin   string           `json:"last_name_latin,omitempty"`
}

type ProfileMutation struct {
	OperationID     uuid.UUID `json:"operation_id"`
	ExpectedVersion int64     `json:"expected_version"`
	Kind            string    `json:"kind"`
	FullName        string    `json:"full_name,omitempty"`
	FirstName       string    `json:"first_name,omitempty"`
	LastName        string    `json:"last_name,omitempty"`
	BotLanguage     string    `json:"bot_language,omitempty"`
	ExpectationID   uuid.UUID `json:"expectation_id,omitempty"`
	FirstNameLatin  *string   `json:"first_name_latin,omitempty"`
	LastNameLatin   *string   `json:"last_name_latin,omitempty"`
}

type ProfileConversationTurn struct {
	Text  string `json:"text"`
	Reply string `json:"reply"`
}

// RecentProfileConversation supplies bounded language evidence from this task's own chat.
func (s *Store) RecentProfileConversation(
	ctx context.Context,
	scope ProfileScope,
	taskID uuid.UUID,
) ([]ProfileConversationTurn, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT left(coalesce(prior.request->>'text',''),512),
left(prior.snapshot#>>'{run,result}',512)
FROM shuttle_tasks current_task JOIN shuttle_tasks prior
 ON prior.conversation_id=current_task.conversation_id AND prior.user_id=current_task.user_id
 AND prior.seq<current_task.seq
WHERE current_task.id=$1 AND current_task.user_id=$2
 AND prior.state IN ('completed','delivery_unknown','delivery_failed','closed')
 AND prior.snapshot#>>'{run,status}' IN ('completed','succeeded')
 AND prior.request->>'kind'='message' AND coalesce(prior.snapshot#>>'{run,result}','')<>''
ORDER BY prior.seq DESC LIMIT 3`, taskID, scope.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	turns := make([]ProfileConversationTurn, 0)
	for rows.Next() {
		var turn ProfileConversationTurn
		if err = rows.Scan(&turn.Text, &turn.Reply); err != nil {
			return nil, err
		}
		turns = append(turns, turn)
	}
	return turns, rows.Err()
}

func lockProfileOwner(ctx context.Context, tx pgx.Tx, scope ProfileScope, write bool) error {
	if scope.BotID <= 0 || scope.Organization == "" || scope.Subject == "" || scope.UserID == uuid.Nil {
		return pgx.ErrNoRows
	}
	query := "SELECT id FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3 AND " +
		"zitadel_subject=$4 AND NOT revoked FOR SHARE"
	if write {
		query = "SELECT id FROM shuttle_users WHERE id=$1 AND bot_id=$2 AND zitadel_org=$3 AND " +
			"zitadel_subject=$4 AND NOT revoked FOR UPDATE"
	}
	var id uuid.UUID
	return tx.QueryRow(ctx, query, scope.UserID, scope.BotID, scope.Organization, scope.Subject).Scan(&id)
}

func readProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID) (UserProfile, error) {
	p := UserProfile{UserID: userID, BotLanguage: profileLanguageAuto}
	var id pgtype.UUID
	var created, expires pgtype.Timestamptz
	err := tx.QueryRow(ctx, "SELECT version,full_name,bot_language,expectation_id,expectation_created_at,"+
		"expectation_expires_at,first_name,last_name,name_source,first_name_latin,"+
		"last_name_latin FROM shuttle_user_profiles WHERE user_id=$1", userID).
		Scan(
			&p.Version,
			&p.FullName,
			&p.BotLanguage,
			&id,
			&created,
			&expires,
			&p.FirstName,
			&p.LastName,
			&p.NameSource,
			&p.FirstNameLatin,
			&p.LastNameLatin,
		)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return p, err
	}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&p.ObservedAt); err != nil {
		return p, err
	}
	p.ObservedAt = p.ObservedAt.UTC()
	if id.Valid && expires.Time.After(p.ObservedAt) {
		p.NameExpectation = &NameExpectation{
			ID:        uuid.UUID(id.Bytes),
			CreatedAt: created.Time.UTC(),
			ExpiresAt: expires.Time.UTC(),
		}
	}
	return p, nil
}

// GetProfile returns an empty version-zero profile for an owner without saved preferences.
func (s *Store) GetProfile(ctx context.Context, scope ProfileScope) (UserProfile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserProfile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return UserProfile{}, err
	}
	return readProfile(ctx, tx, scope.UserID)
}

func normalizeProfileMutation(m ProfileMutation) (ProfileMutation, error) {
	if m.OperationID == uuid.Nil || m.ExpectedVersion < 0 {
		return m, ErrInvalidProfile
	}
	switch m.Kind {
	case profileSetName, profileCompleteName:
		return normalizeProfileNameMutation(m)
	case profileSetLanguage:
		if m.BotLanguage != profileLanguageAuto && m.BotLanguage != "en" && m.BotLanguage != "ru" {
			return m, ErrInvalidProfile
		}
		if m.FullName != "" || m.ExpectationID != uuid.Nil {
			return m, ErrInvalidProfile
		}
	case "request_name":
		if m.FullName != "" || m.BotLanguage != "" || m.ExpectationID != uuid.Nil {
			return m, ErrInvalidProfile
		}
	case "cancel_name":
		if m.FullName != "" || m.BotLanguage != "" || m.ExpectationID == uuid.Nil {
			return m, ErrInvalidProfile
		}
	default:
		return m, ErrInvalidProfile
	}
	if m.FirstName != "" || m.LastName != "" || m.FirstNameLatin != nil || m.LastNameLatin != nil {
		return m, ErrInvalidProfile
	}
	return m, nil
}

func normalizeProfileNameMutation(m ProfileMutation) (ProfileMutation, error) {
	if m.FirstName != "" || m.LastName != "" {
		m.FirstName, m.LastName = strings.TrimSpace(m.FirstName), strings.TrimSpace(m.LastName)
		if !validProfileNameComponent(m.FirstName) || !validProfileNameComponent(m.LastName) || m.FullName != "" {
			return m, ErrInvalidProfile
		}
		m.FullName = m.FirstName + " " + m.LastName
	}
	m.FullName = strings.TrimSpace(m.FullName)
	if !utf8.ValidString(m.FullName) || m.FullName == "" || utf8.RuneCountInString(m.FullName) > 256 ||
		strings.IndexFunc(m.FullName, unicode.IsControl) >= 0 || m.BotLanguage != "" ||
		(m.Kind == profileSetName) != (m.ExpectationID == uuid.Nil) {
		return m, ErrInvalidProfile
	}
	return normalizeProfileLatin(m)
}

func validProfileNameComponent(value string) bool {
	return value != "" && utf8.ValidString(value) && utf8.RuneCountInString(value) <= 200 &&
		!strings.ContainsFunc(value, unicode.IsControl)
}

// MutateProfile serializes owner changes with revocation and saves immutable receipts.
// Repeated values and requests for an existing live expectation keep the version.
func (s *Store) MutateProfile(ctx context.Context, scope ProfileScope, mutation ProfileMutation) (UserProfile, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserProfile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, true); err != nil {
		return UserProfile{}, err
	}
	p, err := mutateProfile(ctx, tx, scope.UserID, mutation)
	if err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}

// mutateProfile requires the caller to hold the owner's write lock.
func mutateProfile(ctx context.Context, tx pgx.Tx, userID uuid.UUID, mutation ProfileMutation) (UserProfile, error) {
	m, err := normalizeProfileMutation(mutation)
	if err != nil {
		return UserProfile{}, err
	}
	request, err := json.Marshal(m)
	if err != nil {
		return UserProfile{}, err
	}
	digest := sha256.Sum256(request)
	prior, err := replayProfileOperation(ctx, tx, userID, m.OperationID, digest[:])
	if !errors.Is(err, pgx.ErrNoRows) {
		return prior, err
	}
	p, err := readProfile(ctx, tx, userID)
	if err != nil {
		return p, err
	}
	if p.Version != m.ExpectedVersion {
		return UserProfile{}, ErrConflict
	}
	nameChanged := m.FirstName != "" &&
		(p.FirstName != m.FirstName || p.LastName != m.LastName)
	if (m.Kind == profileSetName || m.Kind == profileCompleteName) && p.NameSource != "" && m.FirstName == "" {
		// A display-only legacy write cannot erase a confirmed structured name.
		return UserProfile{}, ErrInvalidProfile
	}
	changed, err := applyProfileMutation(ctx, tx, &p, m)
	if err != nil {
		return p, err
	}
	if changed {
		p.Version++
	}
	if changed || m.Kind == profileSetName {
		if err = saveProfileWithLatin(ctx, tx, p); err != nil {
			return p, err
		}
	}
	if err = saveProfileReceipt(ctx, tx, m.OperationID, digest[:], p); err != nil {
		return p, err
	}
	if nameChanged {
		_, err = tx.Exec(ctx, "INSERT INTO shuttle_profile_name_sync(user_id,desired_version) VALUES($1,$2)\n"+
			"ON CONFLICT(user_id) DO UPDATE SET desired_version=EXCLUDED.desired_version,"+
			"next_attempt_at=clock_timestamp(),updated_at=clock_timestamp()",
			userID, p.Version)
		if err != nil {
			return p, err
		}
	}
	return p, nil
}

func replayProfileOperation(
	ctx context.Context,
	tx pgx.Tx,
	userID, operationID uuid.UUID,
	digest []byte,
) (UserProfile, error) {
	var hash, receipt []byte
	err := tx.QueryRow(ctx, "SELECT request_hash,result FROM shuttle_profile_operations WHERE user_id=$1 AND "+
		"operation_id=$2", userID, operationID).
		Scan(&hash, &receipt)
	if err != nil {
		return UserProfile{}, err
	}
	if !bytes.Equal(hash, digest) {
		return UserProfile{}, ErrConflict
	}
	var result UserProfile
	err = json.Unmarshal(receipt, &result)
	return result, err
}

func applyProfileMutation(ctx context.Context, tx pgx.Tx, p *UserProfile, m ProfileMutation) (bool, error) {
	switch m.Kind {
	case profileSetName:
		latinChanged := applyProfileLatin(p, m)
		changed := p.FullName != m.FullName || p.NameExpectation != nil ||
			(m.FirstName != "" && (p.FirstName != m.FirstName || p.LastName != m.LastName ||
				p.NameSource != profileNameSourceUser)) || latinChanged
		p.FullName, p.NameExpectation = m.FullName, nil
		applyProfileNameComponents(p, m)
		return changed, nil
	case profileSetLanguage:
		changed := p.BotLanguage != m.BotLanguage
		p.BotLanguage = m.BotLanguage
		return changed, nil
	case "request_name":
		if p.NameExpectation != nil {
			return false, nil
		}
		e, err := newNameExpectation(ctx, tx, p.ObservedAt)
		if err != nil {
			return false, err
		}
		p.NameExpectation = e
		return true, nil
	case profileCompleteName, "cancel_name":
		if p.NameExpectation == nil || p.NameExpectation.ID != m.ExpectationID {
			return false, ErrConflict
		}
		if m.Kind == profileCompleteName {
			applyProfileLatin(p, m)
			p.FullName = m.FullName
			applyProfileNameComponents(p, m)
		}
		p.NameExpectation = nil
		return true, nil
	}
	return false, ErrInvalidProfile
}

func applyProfileNameComponents(p *UserProfile, m ProfileMutation) {
	if m.FirstName != "" {
		p.FirstName, p.LastName, p.NameSource = m.FirstName, m.LastName, profileNameSourceUser
	}
}

func newNameExpectation(ctx context.Context, tx pgx.Tx, now time.Time) (*NameExpectation, error) {
	var document []byte
	if err := tx.QueryRow(ctx, `SELECT document FROM shuttle_settings WHERE id=true`).Scan(&document); err != nil {
		return nil, err
	}
	var settings Settings
	if err := json.Unmarshal(document, &settings); err != nil {
		return nil, err
	}
	seconds := settings.NameExpectationSeconds
	if seconds == 0 {
		seconds = defaultNameExpectationSeconds
	}
	if seconds < 60 || seconds > 86400 {
		return nil, ErrInvalidProfile
	}
	return &NameExpectation{
		ID: uuid.New(), CreatedAt: now, ExpiresAt: now.Add(time.Duration(seconds) * time.Second),
	}, nil
}

func saveProfile(ctx context.Context, tx pgx.Tx, p UserProfile) error {
	var id any
	var created, expires any
	if e := p.NameExpectation; e != nil {
		id, created, expires = e.ID, e.CreatedAt, e.ExpiresAt
	}
	_, err := tx.Exec(
		ctx,
		"INSERT INTO shuttle_user_profiles(user_id,version,full_name,bot_language,"+
			"expectation_id,expectation_created_at,expectation_expires_at,first_name,last_name,"+
			"name_source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(user_id) DO UPDATE "+
			"SET version=EXCLUDED.version,full_name=EXCLUDED.full_name,"+
			"bot_language=EXCLUDED.bot_language,expectation_id=EXCLUDED.expectation_id,"+
			"expectation_created_at=EXCLUDED.expectation_created_at,"+
			"expectation_expires_at=EXCLUDED.expectation_expires_at,first_name=EXCLUDED.first_name,"+
			"last_name=EXCLUDED.last_name,name_source=EXCLUDED.name_source",
		p.UserID,
		p.Version,
		p.FullName,
		p.BotLanguage,
		id,
		created,
		expires,
		p.FirstName,
		p.LastName,
		p.NameSource,
	)
	return err
}

func saveProfileReceipt(ctx context.Context, tx pgx.Tx, operationID uuid.UUID, digest []byte, p UserProfile) error {
	receipt, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO shuttle_profile_operations(user_id,operation_id,request_hash,result) VALUES($1,$2,$3,$4)`,
		p.UserID,
		operationID,
		digest,
		receipt,
	)
	return err
}

// ProfileOperation returns a historical receipt, never a fresh profile snapshot.
func (s *Store) ProfileOperation(ctx context.Context, scope ProfileScope, operationID uuid.UUID) (UserProfile, error) {
	if operationID == uuid.Nil {
		return UserProfile{}, ErrInvalidProfile
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return UserProfile{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, scope, false); err != nil {
		return UserProfile{}, err
	}
	var receipt []byte
	if err = tx.QueryRow(
		ctx,
		"SELECT result FROM shuttle_profile_operations WHERE user_id=$1 AND operation_id=$2",
		scope.UserID,
		operationID,
	).
		Scan(&receipt); err != nil {
		return UserProfile{}, err
	}
	var p UserProfile
	err = json.Unmarshal(receipt, &p)
	return p, err
}

// ProfileLanguageByTelegram resolves preferences before intake without creating an account.
func (s *Store) ProfileLanguageByTelegram(
	ctx context.Context,
	botID int64,
	organization string,
	telegramID int64,
) (string, error) {
	var language string
	var revoked bool
	err := s.pool.QueryRow(ctx, "SELECT coalesce(p.bot_language,'auto'),u.revoked FROM shuttle_users u LEFT JOIN "+
		"shuttle_user_profiles p ON p.user_id=u.id WHERE u.bot_id=$1 AND u.tg_user_id=$2 AND "+
		"(u.zitadel_org=$3 OR u.zitadel_org='')", botID, telegramID, organization).
		Scan(&language, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return profileLanguageAuto, nil
	}
	if err != nil {
		return "", err
	}
	if revoked {
		return "", pgx.ErrNoRows
	}
	return language, nil
}
