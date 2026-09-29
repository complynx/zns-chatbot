package account

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
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
	input := LanguageChange{Language: language, Initialize: initialize, OperationKey: key}
	if _, err := normalizeLanguageChange(input); err != nil {
		return Preferences{}, err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return Preferences{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }() // Cleanup only; return the operation error.
	prepared, err := s.PrepareLanguageInTx(ctx, tx, owner, input)
	if err != nil {
		return Preferences{}, err
	}
	if value, found := prepared.Replay(); found {
		return value, nil
	}
	value, err := prepared.Apply(ctx)
	if err != nil {
		return value, err
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
	return false, nil
}
