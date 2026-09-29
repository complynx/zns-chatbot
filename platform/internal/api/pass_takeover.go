package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passTakeoverRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	mux.HandleFunc(
		"GET /v1/passes/events/{event}/takeover/{telegram_id}",
		func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("telegram_id"), 10, 64)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.TakeoverTarget(r.Context(), requestOwner(r), r.PathValue("event"), id)
			respondPassProfile(logger, w, value, err)
		},
	)
}
