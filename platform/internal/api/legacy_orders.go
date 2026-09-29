package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func legacyOrderRoutes(mux *http.ServeMux, service orders.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /v1/legacy-order-callbacks/resolve", func(w http.ResponseWriter, r *http.Request) {
		var request orders.LegacyCallbackRequest
		if Decode(w, r, &request) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		binding, err := service.ResolveLegacyCallback(r.Context(), requestOwner(r), request)
		respond(logger, w, binding, err)
	})
}
