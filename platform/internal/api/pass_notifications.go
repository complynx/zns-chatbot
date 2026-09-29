package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

// passNotificationRoutes is mounted only inside the service-authenticated mux.
func passNotificationRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	passAnnouncementRoutes(mux, service, logger)
	mux.HandleFunc("POST /internal/pass-notifications/prepare", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID int64 `json:"id"`
		}
		if Decode(w, r, &input) != nil || input.ID <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		notice, found, err := service.PrepareNotification(r.Context(), input.ID)
		respond(logger, w, struct {
			Notice passbooking.Notification `json:"notice"`
			Found  bool                     `json:"found"`
		}{notice, found}, err)
	})
	mux.HandleFunc("POST /internal/pass-notifications/recover", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.RecoveryNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	mux.HandleFunc("GET /internal/pass-notifications", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.PendingNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	mux.HandleFunc("GET /internal/pass-notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.NotificationStatus(r.Context(), id)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /internal/pass-notifications/begin", func(w http.ResponseWriter, r *http.Request) {
		var input delivery.Attempt
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.BeginNotification(r.Context(), input)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /internal/pass-notifications/complete", func(w http.ResponseWriter, r *http.Request) {
		var input passbooking.NotificationCompletion
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteNotification(r.Context(), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.HandleFunc("POST /internal/pass-notifications/followup", func(w http.ResponseWriter, r *http.Request) {
		var input passbooking.NotificationFollowup
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteNotificationFollowup(r.Context(), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
