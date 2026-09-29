package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/massage"
)

// massageNotificationRoutes is mounted only inside the service-authenticated mux.
func massageNotificationRoutes(mux *http.ServeMux, service massage.Service, logger *slog.Logger) {
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
	mux.HandleFunc(
		"POST /internal/massage-notifications/{owner}/{id}/complete",
		func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
			if err != nil || id <= 0 {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			err = service.AcknowledgeNotice(r.Context(), r.PathValue("owner"), id)
			respond(logger, w, map[string]bool{"ok": err == nil}, err)
		},
	)
}
