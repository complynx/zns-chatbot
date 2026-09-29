package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

// massageNotificationRoutes is mounted only inside the service-authenticated mux.
func massageNotificationRoutes(mux *http.ServeMux, service massage.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /internal/massage-notifications/prepare", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID int64 `json:"id"`
		}
		if Decode(w, r, &input) != nil || input.ID <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		notice, found, err := service.PrepareNotification(r.Context(), input.ID)
		respond(logger, w, struct {
			Notice massage.DeliveryNotice `json:"notice"`
			Found  bool                   `json:"found"`
		}{notice, found}, err)
	})
	mux.HandleFunc("POST /internal/massage-notifications/recover", func(w http.ResponseWriter, r *http.Request) {
		notices, err := service.RecoveryNotifications(r.Context())
		respond(logger, w, notices, err)
	})
	mux.HandleFunc("GET /internal/massage-notifications", func(w http.ResponseWriter, r *http.Request) {
		value, err := service.NoticeRecipients(r.Context())
		respond(logger, w, value, err)
	})
	mux.HandleFunc(
		"POST /internal/massage-notifications/{owner}/pending",
		func(w http.ResponseWriter, r *http.Request) {
			value, err := service.DeliveryNotices(r.Context(), r.PathValue("owner"))
			respond(logger, w, value, err)
		},
	)
	mux.HandleFunc("GET /internal/massage-notifications/{owner}/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.NotificationStatus(r.Context(), r.PathValue("owner"), id)
		respond(logger, w, value, err)
	})
	mux.HandleFunc("POST /internal/massage-notifications/{owner}/begin", func(w http.ResponseWriter, r *http.Request) {
		var input delivery.Attempt
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, err := service.BeginNotification(r.Context(), r.PathValue("owner"), input)
		respond(logger, w, value, err)
	})
	mux.HandleFunc(
		"POST /internal/massage-notifications/{owner}/complete",
		func(w http.ResponseWriter, r *http.Request) {
			var input massage.NotificationCompletion
			if Decode(w, r, &input) != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			err := service.CompleteNotification(r.Context(), r.PathValue("owner"), input)
			respond(logger, w, map[string]bool{"ok": err == nil}, err)
		},
	)
	mux.HandleFunc(
		"POST /internal/massage-notifications/{owner}/followup",
		func(w http.ResponseWriter, r *http.Request) {
			var input massage.NotificationFollowup
			if Decode(w, r, &input) != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			err := service.CompleteNotificationFollowup(r.Context(), r.PathValue("owner"), input)
			respond(logger, w, map[string]bool{"ok": err == nil}, err)
		},
	)
}
