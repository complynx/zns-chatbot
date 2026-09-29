package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/adminmessage"
)

func adminInputExpiryCurrentRoute(mux *http.ServeMux, service adminmessage.Service, logger *slog.Logger) {
	mux.HandleFunc("POST /internal/admin-messages/input-expiry/current", func(w http.ResponseWriter, r *http.Request) {
		var input struct {
			ID int64 `json:"id"`
		}
		if Decode(w, r, &input) != nil || input.ID <= 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		value, found, err := service.CurrentInputExpiry(r.Context(), input.ID)
		respond(logger, w, struct {
			Expiry adminmessage.InputExpiry `json:"expiry"`
			Found  bool                     `json:"found"`
		}{value, found}, err)
	})
}
