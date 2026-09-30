package api

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
)

func passProfileRoutes(mux *http.ServeMux, service passes.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/me/pass-profile/history/page", func(w http.ResponseWriter, r *http.Request) {
		var before int64
		if raw := r.URL.Query().Get("before"); raw != "" {
			value, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || value < 0 {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: "invalid_cursor"})
				return
			}
			before = value
		}
		page, err := service.HistoryPage(r.Context(), requestOwner(r), before)
		respondPassProfile(logger, w, page, err)
	})
	mux.HandleFunc("GET /v1/me/pass-profile/history", func(w http.ResponseWriter, r *http.Request) {
		history, err := service.History(r.Context(), requestOwner(r))
		respondPassProfile(logger, w, history, err)
	})
	mux.HandleFunc("GET /v1/me/pass-profile", func(w http.ResponseWriter, r *http.Request) {
		profile, err := service.Get(r.Context(), requestOwner(r))
		respondPassProfile(logger, w, profile, err)
	})
	mux.HandleFunc("POST /v1/me/pass-profile/actions", func(w http.ResponseWriter, r *http.Request) {
		var command passes.Command
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		profile, err := service.Execute(r.Context(), requestOwner(r), command)
		respondPassProfile(logger, w, profile, err)
	})
}

// Database errors can contain rejected identity values. Keep their details out
// of both responses and application logs for this sensitive endpoint.
func respondPassProfile(logger *slog.Logger, w http.ResponseWriter, value any, err error) {
	if err == nil {
		JSON(w, http.StatusOK, value)
		return
	}
	markDatabaseFailure(w, err)
	if p, ok := errors.AsType[*core.ProblemError](err); ok {
		JSON(w, p.Status, p)
		return
	}
	logger.Error("Pass profile request failed")
	JSON(w, http.StatusInternalServerError, map[string]string{codeField: internalError})
}
