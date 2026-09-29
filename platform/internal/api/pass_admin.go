package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passAdminRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	passAdminReadRoute(mux, service, logger)
	mux.HandleFunc("POST /v1/passes/admin/assign", func(w http.ResponseWriter, r *http.Request) {
		var command passbooking.AdminAssignment
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		result, err := service.AdminAssign(r.Context(), requestOwner(r), command)
		respond(logger, w, result, err)
	})
}
