package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passNavigationRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	mux.HandleFunc("GET /v1/passes/bookings/owned", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.OwnsEventBookings(r.Context(), requestOwner(r), r.URL.Query()["event"])
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/page", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.EventsPage(r.Context(), requestOwner(r), r.URL.Query().Get("cursor"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/detail", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.EventDetail(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("cursor"),
		)
		respond(logger, w, value, err)
	})
}
