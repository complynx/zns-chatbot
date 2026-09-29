package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passBatchRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/passes/events/{event}/tiers", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.TierStatus(r.Context(), requestOwner(r), r.PathValue("event"))
		respond(logger, w, result, err)
	})
	mux.HandleFunc("POST /v1/passes/batches", func(w http.ResponseWriter, r *http.Request) {
		var command passbooking.RuntimeBatch
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		result, err := service.RunBatch(r.Context(), requestOwner(r), command)
		respond(logger, w, result, err)
	})
}
