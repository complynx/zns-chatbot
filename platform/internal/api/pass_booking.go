package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/derivedmutation"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passBookingRoutes(
	mux *http.ServeMux,
	service passbooking.Service,
	files orders.Service,
	derived derivedmutation.Service,
	logger *slog.Logger,
) {
	passCapabilitiesRoute(mux, service, logger)
	passBatchRoutes(mux, service, derived, logger)
	passExportRoute(mux, service, logger)
	passTakeoverRoutes(mux, service, logger)
	passAdminRoutes(mux, service, logger)
	passPaymentRoutes(mux, service, files, logger)
	mux.HandleFunc("GET /v1/passes/events", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Events(r.Context(), requestOwner(r))
		respondPassProfile(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/me", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Get(r.Context(), requestOwner(r), r.PathValue("event"))
		respondPassProfile(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/invitations", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Invitations(
			r.Context(),
			requestOwner(r),
			r.PathValue("event"),
			r.URL.Query().Get("after"),
		)
		respondPassProfile(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/payment-admins", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.PaymentAdmins(r.Context(), requestOwner(r), r.PathValue("event"))
		respondPassProfile(logger, w, value, err)
	})
	mux.HandleFunc("GET /v1/passes/events/{event}/queue", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.Queue(r.Context(), requestOwner(r), r.PathValue("event"), r.URL.Query().Get("after"))
		respondPassProfile(logger, w, value, err)
	})
	mux.HandleFunc("POST /v1/passes/actions", func(w http.ResponseWriter, r *http.Request) {
		var command passbooking.Command
		if Decode(w, r, &command) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.Execute(r.Context(), requestOwner(r), command)
		respondPassProfile(logger, w, value, err)
	})
}
