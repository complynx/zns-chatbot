package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func privilegedReadRoutes(
	mux *http.ServeMux,
	service core.Service,
	passes passbooking.Service,
	practitioners massage.Service,
	logger *slog.Logger,
) {
	mux.HandleFunc("GET /v1/privileged-read-capabilities", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.PrivilegedReads(r.Context(), requestOwner(r))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/privileged-read-events", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.PrivilegedReadEvents(r.Context(), requestOwner(r), r.URL.Query().Get("cursor"))
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/payment-history", func(w http.ResponseWriter, r *http.Request) {
		value, err := passes.PaymentHistoryPage(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("cursor"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/practitioner/schedule", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		value, err := practitioners.PractitionerSchedule(
			r.Context(),
			requestOwner(r),
			query.Get("event"),
			query.Get("cursor"),
		)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/massage/practitioner/bookings", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		value, err := practitioners.PractitionerBookings(
			r.Context(),
			requestOwner(r),
			query.Get("event"),
			query.Get("party"),
			query.Get("cursor"),
		)
		respond(logger, w, value, err)
	})
}
