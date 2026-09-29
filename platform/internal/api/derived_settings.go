package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/account"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/credits"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func derivedSettingsRoutes(
	mux *http.ServeMux,
	service derivedmutation.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle(
		"POST /internal/derived/preferences/language",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input account.LanguageChange
			source, err := decodeDerivedMutation(w, r, &input, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.SetLanguage(r.Context(), requestOwner(r), input, source)
			respond(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/model-settings/{scope}",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input modelsettings.Change
			source, err := decodeDerivedMutation(w, r, &input, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.SetModelSettings(r.Context(), requestOwner(r), r.PathValue("scope"), input, source)
			respond(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/model-grants",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input modelsettings.Grant
			source, err := decodeDerivedMutation(w, r, &input, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			err = service.GrantModelSettings(r.Context(), requestOwner(r), input, source)
			respond(logger, w, map[string]bool{"ok": err == nil}, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/credit-policy/{payer}",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input credits.PolicyChange
			source, err := decodeDerivedMutation(w, r, &input, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.SetCreditPolicy(r.Context(), requestOwner(r), r.PathValue("payer"), input, source)
			respond(logger, w, value, creditProblem(err))
		}), logger),
	)
}
