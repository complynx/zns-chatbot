package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func modelSettingsRoutes(mux *http.ServeMux, settings modelsettings.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/model-settings/effective", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		value, err := settings.Effective(r.Context(), requestOwner(r))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/model-settings/permissions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		value, err := settings.Permissions(r.Context(), requestOwner(r))
		respond(logger, w, value, err)
	})
	for _, route := range []struct {
		path  string
		scope func(*http.Request) string
	}{
		{"/v1/model-settings", requestOwner},
		{"/v1/model-settings/default", func(*http.Request) string { return modelsettings.GlobalScope }},
		{"/v1/model-settings/users/{owner}", func(r *http.Request) string { return r.PathValue("owner") }},
	} {
		mux.HandleFunc("GET "+route.path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			result, err := settings.Read(r.Context(), requestOwner(r), route.scope(r))
			respond(logger, w, result, err)
		})
		mux.HandleFunc("POST "+route.path, func(w http.ResponseWriter, r *http.Request) {
			var input modelsettings.Change
			if Decode(w, r, &input) != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := settings.Set(r.Context(), requestOwner(r), route.scope(r), input)
			respond(logger, w, result, err)
		})
	}
	mux.HandleFunc("POST /v1/model-settings/grants", func(w http.ResponseWriter, r *http.Request) {
		var input modelsettings.Grant
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := settings.Grant(r.Context(), requestOwner(r), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
