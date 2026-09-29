package appclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Host) SetDerivedLanguage(
	ctx context.Context,
	owner string,
	command account.LanguageChange,
	source readsource.Derivation,
) (account.Preferences, error) {
	var result account.Preferences
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (account.Preferences, error) {
				return s.SetLanguage(ctx, actor, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(ctx, owner, "/internal/derived/preferences/language", command, source, &result)
	return result, err
}

func (c Host) SetDerivedModelSettings(
	ctx context.Context,
	owner, scope string,
	command modelsettings.Change,
	source readsource.Derivation,
) (modelsettings.State, error) {
	var result modelsettings.State
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (modelsettings.State, error) {
				return s.SetModelSettings(ctx, actor, scope, command, source.Clone())
			}),
		)
	}
	err := c.derivedRequest(
		ctx,
		owner,
		"/internal/derived/model-settings/"+url.PathEscape(scope),
		command,
		source,
		&result,
	)
	return result, err
}

func (c Host) SetDerivedCreditPolicy(
	ctx context.Context,
	owner, payer string,
	command credits.PolicyChange,
	source readsource.Derivation,
) (credits.Policy, error) {
	var result credits.Policy
	if err := validateDerivedCommand(command, source); err != nil {
		return result, err
	}
	if c.LocalDerived != nil {
		return boundedDerivedResult(
			directDerived(ctx, c, owner, func(s derivedmutation.Service, actor string) (credits.Policy, error) {
				value, err := s.SetCreditPolicy(ctx, actor, payer, command, source.Clone())
				return value, derivedCreditProblem(err)
			}),
		)
	}
	err := c.derivedRequest(
		ctx,
		owner,
		"/internal/derived/credit-policy/"+url.PathEscape(payer),
		command,
		source,
		&result,
	)
	return result, err
}

type derivedGrantResult struct {
	OK bool `json:"ok"`
}

func (c Host) GrantDerivedModelSettings(
	ctx context.Context,
	owner string,
	command modelsettings.Grant,
	source readsource.Derivation,
) error {
	if err := validateDerivedCommand(command, source); err != nil {
		return err
	}
	if c.LocalDerived != nil {
		_, err := directDerived(
			ctx,
			c,
			owner,
			func(s derivedmutation.Service, actor string) (derivedGrantResult, error) {
				err := s.GrantModelSettings(ctx, actor, command, source.Clone())
				return derivedGrantResult{OK: err == nil}, err
			},
		)
		return err
	}
	var result derivedGrantResult
	return c.derivedRequest(ctx, owner, "/internal/derived/model-grants", command, source, &result)
}

// Match the public command bound; host evidence has its own independent bound.
func validateDerivedCommand[T any](command T, source readsource.Derivation) error {
	const maxCommandBytes = 64 << 10
	return validateDerivedCommandLimit(command, source, maxCommandBytes)
}

func validateDerivedCommandLimit[T any](command T, source readsource.Derivation, maxCommandBytes int) error {
	body, err := json.Marshal(command)
	if err != nil || len(body) > maxCommandBytes || !source.Valid() {
		return &core.ProblemError{Status: http.StatusBadRequest, Code: invalidJSONCode}
	}
	return nil
}

// JSON responses include the encoder's final newline on the HTTP path.
func boundedDerivedResult[T any](value T, err error) (T, error) {
	if err != nil {
		return value, err
	}
	body, err := json.Marshal(value)
	if err != nil {
		var zero T
		return zero, orderApplicationError(err)
	}
	if len(body)+1 > MaxAPIBytes {
		var zero T
		return zero, &apiResponseLimitError{}
	}
	return value, nil
}

func derivedCreditProblem(err error) error {
	switch {
	case errors.Is(err, credits.ErrInvalid):
		return &core.ProblemError{Status: http.StatusBadRequest, Code: "credits_invalid"}
	case errors.Is(err, credits.ErrConflict):
		return &core.ProblemError{Status: http.StatusConflict, Code: "credits_conflict"}
	default:
		return err
	}
}
