package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func passOperationRoutes(
	mux *http.ServeMux,
	service derivedmutation.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle("POST /internal/derived/pass-operation", authenticatedDerivation(authorizer, signer,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input derivedmutation.PassOperationInput
			if err := decodeDerivedJSON(
				http.MaxBytesReader(w, r.Body, readsource.MaxAuthorityBytes+maxRequestBytes),
				&input,
			); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.ReadPassOperation(r.Context(), requestOwner(r), input)
			respond(logger, w, result, err)
		}), logger))
}
