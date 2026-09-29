package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func notificationRoutes(
	mux *http.ServeMux,
	service orders.Service,
	massageService massage.Service,
	registrations passbooking.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	queue := http.NewServeMux()
	massageNotificationRoutes(queue, massageService, logger)
	passNotificationRoutes(queue, registrations, logger)
	queue.HandleFunc("POST /internal/notifications/prepare", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID int64 `json:"id"`
		}
		if Decode(w, r, &input) != nil || input.ID <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		notice, found, err := service.PrepareNotification(r.Context(), input.ID)
		respond(logger, w, struct {
			Notice orders.Notification `json:"notice"`
			Found  bool                `json:"found"`
		}{notice, found}, err)
	})
	queue.HandleFunc("POST /internal/notifications/recover", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.RecoveryNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	queue.HandleFunc("GET /internal/notifications", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.PendingNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	queue.HandleFunc("GET /internal/notifications/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.NotificationStatus(r.Context(), id)
		respond(logger, w, value, err)
	})
	queue.HandleFunc("POST /internal/notifications/begin", func(w http.ResponseWriter, r *http.Request) {
		var input delivery.Attempt
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.BeginNotification(r.Context(), input)
		respond(logger, w, value, err)
	})
	queue.HandleFunc("POST /internal/notifications/complete", func(w http.ResponseWriter, r *http.Request) {
		var input orders.NotificationCompletion
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteNotification(r.Context(), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	queue.HandleFunc("POST /internal/notifications/followup", func(w http.ResponseWriter, r *http.Request) {
		var input orders.NotificationFollowup
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteNotificationFollowup(r.Context(), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.Handle("/internal/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if signer.VerifyDelivery(token) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		queue.ServeHTTP(w, r)
	}))
}
