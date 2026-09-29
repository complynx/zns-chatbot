package api

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// Username is only a delivery address. Consent from that Telegram sender is
// still required, and ambiguous/stale names never resolve to an arbitrary row.
func browserAuthRoutes(mux *http.ServeMux, service core.Service, signer identity.Signer, logger *slog.Logger) {
	mux.HandleFunc("POST /internal/browser-auth/recipient", func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || signer.VerifyDelivery(token) != nil {
			JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
			return
		}
		var input struct {
			Username string `json:"username"`
		}
		if Decode(w, r, &input) != nil || len(input.Username) > 64 || input.Username == "" {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		var result struct {
			TelegramID int64  `json:"telegram_id"`
			Language   string `json:"language"`
		}
		err := service.DB.QueryRow(r.Context(), `SELECT COALESCE(min(telegram_id),0), COALESCE(min(language),'en') FROM core.users WHERE lower(username)=lower($1) HAVING count(*)=1`, input.Username).
			Scan(&result.TelegramID, &result.Language)
		respond(logger, w, result, err)
	})
}
