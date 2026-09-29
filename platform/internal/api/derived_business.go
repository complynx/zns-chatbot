package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func derivedBusinessRoutes(
	mux *http.ServeMux,
	service derivedmutation.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle(
		"POST /internal/derived/food-actions",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command legacyfood.Command
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecuteFood(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/massage-actions",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command massage.Command
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecuteMassage(r.Context(), requestOwner(r), command, source)
			respond(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/derived/massage-preferences",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command massage.Preferences
			source, err := decodeDerivedMutation(w, r, &command, maxRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.SetMassagePreferences(
				r.Context(),
				requestOwner(r),
				r.URL.Query().Get("event"),
				command,
				source,
			)
			respond(logger, w, value, err)
		}), logger),
	)
}
