package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passCapabilitiesRoute(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/passes/tool-capabilities", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ToolCapabilities(r.Context(), requestOwner(r))
		respondPassProfile(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/capabilities", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Capabilities(r.Context(), requestOwner(r), r.PathValue("event"))
		respondPassProfile(logger, w, value, err)
	})
}
