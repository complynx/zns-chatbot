package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

func massageRoutes(mux *http.ServeMux, service massage.Service, logger *slog.Logger) {
	massageLegacyRoutes(mux, service, logger)
	mux.HandleFunc("GET /v1/massage/provider-names", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.ProviderNames(r.Context(), requestOwner(r), r.URL.Query().Get("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/timetable", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Timetable(r.Context(), requestOwner(r), r.URL.Query().Get("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/parties", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Parties(r.Context(), requestOwner(r), r.URL.Query().Get("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/slots", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		length, err := strconv.Atoi(query.Get("length"))
		if err != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Slots(r.Context(), requestOwner(r), query.Get("event"), query.Get("party"), length)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/bookings", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		value, err := service.Bookings(
			r.Context(),
			requestOwner(r),
			query.Get("event"),
			query.Get("party"),
			query.Get("view"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/massage/actions", func(w http.ResponseWriter, r *http.Request) {
		var command massage.Command
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Execute(r.Context(), requestOwner(r), command)
		respond(logger, w, value, err)
	})
	massagePreferenceRoutes(mux, service, logger)
}

func massagePreferenceRoutes(mux *http.ServeMux, service massage.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/massage/preferences", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Preferences(r.Context(), requestOwner(r), r.URL.Query().Get("event"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("PUT /v1/massage/preferences", func(w http.ResponseWriter, r *http.Request) {
		var preferences massage.Preferences
		if Decode(w, r, &preferences) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.SetPreferences(r.Context(), requestOwner(r), r.URL.Query().Get("event"), preferences)
		respond(logger, w, value, err)
	})
}
