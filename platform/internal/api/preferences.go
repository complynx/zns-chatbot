package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

func preferenceRoutes(mux *http.ServeMux, service core.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/me/preferences", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Preferences(r.Context(), requestOwner(r))
		w.Header().Set("Cache-Control", "no-store")
		respond(logger, w, value, err)
	})
	mux.HandleFunc("PUT /v1/me/preferences/language", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			OperationKey core.LanguageOperationKey `json:"operation_key,omitempty"`
			Language     string                    `json:"language"`
			Initialize   bool                      `json:"initialize"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.SetLanguageWithOperation(
			r.Context(),
			requestOwner(r),
			input.Language,
			input.Initialize,
			input.OperationKey,
		)
		w.Header().Set("Cache-Control", "no-store")
		respond(logger, w, value, err)
	})
}
