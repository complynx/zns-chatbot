package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passExportAuthorityRoutes(
	mux *http.ServeMux,
	service passbooking.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle(
		"GET /internal/passes/export-snapshot",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			value, err := service.ExportSnapshot(r.Context(), requestOwner(r))
			respond(logger, w, value, err)
		}), logger),
	)
	mux.Handle(
		"POST /internal/passes/export-authority",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input struct {
				Events []string `json:"events"`
			}
			if err := decodeDerivedJSON(
				http.MaxBytesReader(w, r.Body, passbooking.MaxExportAuthorityBytes),
				&input,
			); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			err := service.CheckExportSnapshot(r.Context(), requestOwner(r), input.Events)
			respond(logger, w, struct{}{}, err)
		}), logger),
	)
}
