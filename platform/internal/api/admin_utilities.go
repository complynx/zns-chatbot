package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/adminutilities"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func adminUtilityRoutes(mux *http.ServeMux, service adminutilities.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/admin-utilities/authorize", func(w http.ResponseWriter, r *http.Request) {
		err := service.Authorize(r.Context(), requestOwner(r))
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.HandleFunc("GET /v1/admin-utilities/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			respond(logger, w, nil, &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_user_id"})
			return
		}
		result, err := service.User(r.Context(), requestOwner(r), id)
		respond(logger, w, result, err)
	})
	mux.HandleFunc("POST /v1/admin-utilities/refresh", func(w http.ResponseWriter, r *http.Request) {
		result, err := service.Refresh(r.Context(), requestOwner(r))
		respond(logger, w, result, err)
	})
}
