package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func adminQueueRoutes(mux *http.ServeMux, s adminmessage.Service, logger *slog.Logger) {
	queuePrepareRoute(
		mux,
		"/internal/admin-messages/prepare",
		s.PrepareDelivery,
		func(value adminmessage.Delivery, found bool) any {
			return struct {
				Delivery adminmessage.Delivery `json:"delivery"`
				Found    bool                  `json:"found"`
			}{value, found}
		},
		logger,
	)
	queueRecoveryRoute(mux, "/internal/admin-messages/recover", s.RecoverDeliveries, logger)
}
func announcementQueueRoutes(mux *http.ServeMux, s passbooking.Service, logger *slog.Logger) {
	queuePrepareRoute(
		mux,
		"/internal/pass-announcements/prepare",
		s.PrepareRegistrationAnnouncement,
		func(value passbooking.RegistrationAnnouncement, found bool) any {
			return struct {
				Announcement passbooking.RegistrationAnnouncement `json:"announcement"`
				Found        bool                                 `json:"found"`
			}{value, found}
		},
		logger,
	)
	queueRecoveryRoute(mux, "/internal/pass-announcements/recover", s.RecoverRegistrationAnnouncements, logger)
}

func queuePrepareRoute[T any](
	mux *http.ServeMux,
	path string,
	prepare func(context.Context, int64) (T, bool, error),
	envelope func(T, bool) any,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID int64 `json:"id"`
		}
		if Decode(w, r, &input) != nil || input.ID <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, found, err := prepare(r.Context(), input.ID)
		respond(logger, w, envelope(value, found), err)
	})
}
func queueRecoveryRoute(mux *http.ServeMux, path string, recoverWork func(context.Context) error, logger *slog.Logger) {
	mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
		err := recoverWork(r.Context())
		respond(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
