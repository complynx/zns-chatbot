package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func massageLegacyRoutes(mux *http.ServeMux, service massage.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /v1/massage/legacy-instant", func(w http.ResponseWriter, r *http.Request) {
		var command massage.LegacyCommand
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.LegacyInstant(r.Context(), requestOwner(r), command.Event, command.Key, command.Length)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/legacy-practitioner-booking", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.LegacyPractitionerBooking(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("event"),
			r.URL.Query().Get("id"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/massage/legacy-preferences", func(w http.ResponseWriter, r *http.Request) {
		var command massage.LegacyCommand
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.LegacyTogglePreferences(
			r.Context(),
			requestOwner(r),
			command.Event,
			command.Key,
			command.Choice,
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/legacy-draft", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.LegacyDraft(
			r.Context(),
			requestOwner(r),
			r.URL.Query().Get("event"),
			r.URL.Query().Get("id"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/massage/legacy-draft", func(w http.ResponseWriter, r *http.Request) {
		var command massage.LegacyCommand
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.ExecuteLegacy(r.Context(), requestOwner(r), command)
		respond(logger, w, value, err)
	})
}
