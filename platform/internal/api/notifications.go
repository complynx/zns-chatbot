package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

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
	delivery := http.NewServeMux()
	massageNotificationRoutes(delivery, massageService, logger)
	passNotificationRoutes(delivery, registrations, logger)
	delivery.HandleFunc("GET /internal/notifications", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.PendingNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	delivery.HandleFunc(
		"POST /internal/notifications/{id}/claim-reminder",
		func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			claimed, err := service.ClaimReminder(r.Context(), id)
			respond(logger, w, map[string]bool{"claimed": claimed}, err)
		},
	)
	delivery.HandleFunc("POST /internal/notifications/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			Failure string `json:"failure"`
		}
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err = service.CompleteNotification(r.Context(), id, input.Failure)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
	mux.Handle("/internal/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if signer.VerifyDelivery(token) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		delivery.ServeHTTP(w, r)
	}))
}
