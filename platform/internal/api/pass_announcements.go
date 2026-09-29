package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func passAnnouncementRoutes(mux *http.ServeMux, service passbooking.Service, logger *slog.Logger) {
	announcementQueueRoutes(mux, service, logger)
	mux.HandleFunc("POST /internal/pass-announcements/claim", func(w http.ResponseWriter, r *http.Request) {
		item, found, err := service.ClaimRegistrationAnnouncement(r.Context())
		respond(logger, w, struct {
			Announcement passbooking.RegistrationAnnouncement `json:"announcement"`
			Found        bool                                 `json:"found"`
		}{item, found}, err)
	})
	mux.HandleFunc("POST /internal/pass-announcements/begin", func(w http.ResponseWriter, r *http.Request) {
		var input delivery.Attempt
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		result, err := service.BeginRegistrationAnnouncement(r.Context(), input)
		respond(logger, w, result, err)
	})
	mux.HandleFunc("POST /internal/pass-announcements/complete", func(w http.ResponseWriter, r *http.Request) {
		var input passbooking.AnnouncementCompletion
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.CompleteRegistrationAnnouncement(r.Context(), input)
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
