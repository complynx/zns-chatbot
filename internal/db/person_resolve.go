package db

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/telegram"
)

var ErrPersonAmbiguous = errors.New("multiple users have this Telegram username")

var personUsername = regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`)

type PersonReference struct {
	UserID         uuid.UUID `json:"user_id,omitempty"`
	TelegramUserID int64     `json:"telegram_user_id,omitempty" minimum:"1"`
	Username       string    `json:"username,omitempty" maxLength:"65"`
}

type PersonIdentity struct {
	UserID         uuid.UUID `json:"user_id"`
	TelegramUserID int64     `json:"telegram_user_id"`
	PreferredName  string    `json:"preferred_name"`
	Username       string    `json:"username,omitempty"`
	ProfileURL     string    `json:"profile_url"`
}

// ResolvePerson exposes only basic current identity for an exact reference,
// including hidden/revoked targets. Directory and administrative access is separate.
func (s *Store) ResolvePerson(ctx context.Context, scope PaymentScope, a PersonReference) (PersonIdentity, error) {
	var result PersonIdentity
	selector, value, err := personReferenceSelector(a)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, peopleReadTimeout)
	defer cancel()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = lockProfileOwner(ctx, tx, ProfileScope{BotID: scope.BotID, Organization: scope.Organization,
		UserID: scope.UserID, Subject: scope.Subject}, false); err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT u.id,u.tg_user_id,`+personNameSQL+`,`+personUsernameSQL+`
 FROM shuttle_users u LEFT JOIN shuttle_user_profiles p ON p.user_id=u.id
 WHERE u.bot_id=$1 AND u.zitadel_org=$2 AND `+selector+` ORDER BY u.id LIMIT 2`,
		scope.BotID, scope.Organization, value)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if err = rows.Scan(&result.UserID, &result.TelegramUserID, &result.PreferredName, &result.Username); err != nil {
			return PersonIdentity{}, err
		}
	}
	if err = rows.Err(); err != nil {
		return PersonIdentity{}, err
	}
	if count == 0 {
		return PersonIdentity{}, ErrPeopleDenied
	}
	if count > 1 {
		return PersonIdentity{}, ErrPersonAmbiguous
	}
	result.PreferredName = PersonPreferredName(result.PreferredName, result.Username, result.TelegramUserID)
	result.ProfileURL = telegram.UserProfileURL(result.TelegramUserID, result.Username)
	return result, nil
}

// PersonPreferredName keeps labels on one line and prevents account-shaped names
// from being interpreted as a reference to another person in agent output.
func PersonPreferredName(name, username string, telegramID int64) string {
	name = strings.Join(strings.Fields(name), " ")
	if name != "" && !nameContainsAccountID(name) {
		return name
	}
	if username != "" {
		return "@" + username
	}
	return fmt.Sprintf("Telegram %d", telegramID)
}

func nameContainsAccountID(name string) bool {
	const uuidTextLength = 36
	for start := 0; start+uuidTextLength <= len(name); start++ {
		if uuid.Validate(name[start:start+uuidTextLength]) == nil {
			return true
		}
	}
	return false
}

func personReferenceSelector(a PersonReference) (string, any, error) {
	selectors := 0
	if a.UserID != uuid.Nil {
		selectors++
	}
	if a.TelegramUserID != 0 {
		selectors++
	}
	if a.Username != "" {
		selectors++
	}
	if selectors != 1 || a.TelegramUserID < 0 {
		return "", nil, ErrPeopleInvalid
	}
	if a.UserID != uuid.Nil {
		return "u.id=$3", a.UserID, nil
	}
	if a.TelegramUserID > 0 {
		return "u.tg_user_id=$3", a.TelegramUserID, nil
	}
	username := strings.TrimPrefix(strings.TrimSpace(a.Username), "@")
	if !personUsername.MatchString(username) {
		return "", nil, ErrPeopleInvalid
	}
	// No index: a deployment has about a thousand users, so a scan is sufficient.
	return "lower(u.profile->>'username')=lower($3)", username, nil
}
