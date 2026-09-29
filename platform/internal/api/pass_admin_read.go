package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passAdminReadRoute(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	mux.HandleFunc(
		"GET /v1/passes/events/{event}/admin/targets/{telegram_id}",
		func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("telegram_id"), 10, 64)
			if err != nil || id <= 0 {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.AdminTarget(r.Context(), requestOwner(r), r.PathValue("event"), id)
			respond(logger, w, result, err)
		},
	)
}
