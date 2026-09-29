package core

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/broadcastprofile"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

type Preferences struct {
	Language string `json:"language"`
}

// LanguageOperationKey identifies one owner-scoped language selection across retries.
type LanguageOperationKey string

func (s Service) Preferences(ctx context.Context, owner string) (Preferences, error) {
	var value Preferences
	err := s.DB.QueryRow(ctx, `SELECT COALESCE(NULLIF(language,''),'en') FROM core.users WHERE id=$1`, owner).
		Scan(&value.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, fail(http.StatusNotFound, "user_not_found")
	}
	return value, err
}

// SetLanguage updates only the authenticated owner's preference. Initialization
// cannot overwrite a language already chosen by the user.
func (s Service) SetLanguage(ctx context.Context, owner, language string, initialize bool) (Preferences, error) {
	return s.SetLanguageWithOperation(ctx, owner, language, initialize, "")
}

// SetLanguageWithOperation commits the selection and retry identity atomically.
// Replays return the current preference, so an older retry cannot undo a newer choice.
func (s Service) SetLanguageWithOperation(
	ctx context.Context,
	owner, language string,
	initialize bool,
	key LanguageOperationKey,
) (Preferences, error) {
	const maxLanguageBytes = 64
	const maxOperationKeyBytes = 128
	sourceLanguage := language
	if len(key) > maxOperationKeyBytes {
		return Preferences{}, fail(http.StatusBadRequest, "invalid_operation_key")
	}
	if len(language) > maxLanguageBytes {
		return Preferences{}, fail(http.StatusBadRequest, "invalid_language")
	}
	if initialize {
		language = string(i18n.FallbackLocales(language)[0])
	} else if !i18n.IsSupported(language) {
		return Preferences{}, fail(http.StatusBadRequest, "invalid_language")
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Preferences{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup only; return the operation error.
	var value Preferences
	var rawLanguage string
	err = tx.QueryRow(ctx, `SELECT language,COALESCE(NULLIF(language,''),'en') FROM core.users WHERE id=$1 FOR UPDATE`, owner).
		Scan(&rawLanguage, &value.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return value, fail(http.StatusNotFound, "user_not_found")
	}
	if err != nil {
		return value, err
	}
	replay, err := languageReplay(ctx, tx, owner, key, language, initialize)
	if err != nil || replay {
		return value, err
	}
	err = tx.QueryRow(ctx, `UPDATE core.users SET language=CASE WHEN $3 AND language<>'' THEN language ELSE $2 END
		WHERE id=$1 RETURNING COALESCE(NULLIF(language,''),'en')`, owner, language, initialize).Scan(&value.Language)
	if err != nil {
		return value, err
	}
	if !initialize || (rawLanguage == "" && sourceLanguage != "") {
		if err = broadcastprofile.Language(ctx, tx, owner, sourceLanguage); err != nil {
			return value, err
		}
	}
	return value, tx.Commit(ctx)
}

func languageReplay(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	key LanguageOperationKey,
	language string,
	initialize bool,
) (bool, error) {
	if key == "" {
		return false, nil
	}
	var previousLanguage string
	var previousInitialize bool
	err := tx.QueryRow(ctx, `SELECT language,initialize FROM core.language_operations WHERE owner=$1 AND operation_key=$2`, owner, key).
		Scan(&previousLanguage, &previousInitialize)
	if err == nil {
		if previousLanguage != language || previousInitialize != initialize {
			return false, fail(http.StatusConflict, "operation_key_reused")
		}
		return true, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	_, err = tx.Exec(
		ctx,
		`INSERT INTO core.language_operations(owner,operation_key,language,initialize) VALUES($1,$2,$3,$4)`,
		owner,
		key,
		language,
		initialize,
	)
	return false, err
}
