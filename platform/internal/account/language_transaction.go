package account

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/broadcastprofile"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

type LanguageChange struct {
	OperationKey LanguageOperationKey `json:"operation_key,omitempty"`
	Language     string               `json:"language"`
	Initialize   bool                 `json:"initialize"`
}

type PreparedLanguage struct {
	tx                    pgx.Tx
	owner                 string
	input                 LanguageChange
	language, rawLanguage string
	value                 Preferences
	found                 bool
}

// PrepareLanguageInTx locks the owner and checks the durable command identity.
// It does not change preferences or create an operation receipt.
func (s Service) PrepareLanguageInTx(
	ctx context.Context,
	tx pgx.Tx,
	owner string,
	input LanguageChange,
) (*PreparedLanguage, error) {
	language, err := normalizeLanguageChange(input)
	if err != nil {
		return nil, err
	}
	p := &PreparedLanguage{tx: tx, owner: owner, input: input, language: language}
	err = tx.QueryRow(ctx, `SELECT language,COALESCE(NULLIF(language,''),'en') FROM core.users WHERE id=$1 FOR UPDATE`, owner).
		Scan(&p.rawLanguage, &p.value.Language)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fail(http.StatusNotFound, "user_not_found")
	}
	if err != nil {
		return nil, core.DatabaseOperationError(err)
	}
	p.found, err = languageReplay(ctx, tx, owner, input.OperationKey, language, input.Initialize)
	return p, err
}

func normalizeLanguageChange(input LanguageChange) (string, error) {
	const maxLanguageBytes = 64
	const maxOperationKeyBytes = 128
	if len(input.OperationKey) > maxOperationKeyBytes {
		return "", fail(http.StatusBadRequest, "invalid_operation_key")
	}
	if len(input.Language) > maxLanguageBytes {
		return "", fail(http.StatusBadRequest, "invalid_language")
	}
	language := input.Language
	if input.Initialize {
		language = string(i18n.FallbackLocales(language)[0])
	} else if !i18n.IsSupported(language) {
		return "", fail(http.StatusBadRequest, "invalid_language")
	}
	return language, nil
}

func (p *PreparedLanguage) Replay() (Preferences, bool) { return p.value, p.found }

func (p *PreparedLanguage) Apply(ctx context.Context) (Preferences, error) {
	if p.found {
		return p.value, nil
	}
	value := p.value
	err := p.tx.QueryRow(ctx, `UPDATE core.users SET language=CASE WHEN $3 AND language<>'' THEN language ELSE $2 END
		WHERE id=$1 RETURNING COALESCE(NULLIF(language,''),'en')`, p.owner, p.language, p.input.Initialize).Scan(&value.Language)
	if err != nil {
		return value, core.DatabaseOperationError(err)
	}
	if !p.input.Initialize || (p.rawLanguage == "" && p.input.Language != "") {
		if err = broadcastprofile.Language(ctx, p.tx, p.owner, p.input.Language); err != nil {
			return value, err
		}
	}
	if p.input.OperationKey != "" {
		_, err = p.tx.Exec(
			ctx,
			`INSERT INTO core.language_operations(owner,operation_key,language,initialize) VALUES($1,$2,$3,$4)`,
			p.owner,
			p.input.OperationKey,
			p.language,
			p.input.Initialize,
		)
		err = core.DatabaseOperationError(err)
	}
	return value, err
}
