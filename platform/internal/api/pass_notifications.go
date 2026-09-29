package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// passNotificationRoutes is mounted only inside the service-authenticated mux.
func passNotificationRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	passAnnouncementRoutes(mux, service, logger)
	mux.HandleFunc("GET /internal/pass-notifications", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.PendingNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	mux.HandleFunc("POST /internal/pass-notifications/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Failure string `json:"failure"`
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 || Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err = service.CompleteNotification(r.Context(), id, input.Failure)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
