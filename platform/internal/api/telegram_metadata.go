package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func telegramMetadataRoutes(mux *http.ServeMux, service core.Service, signer identity.Signer, logger *slog.Logger) {
	mux.HandleFunc("POST /internal/telegram/metadata", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input core.TelegramMetadataUpdate
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.RefreshTelegramMetadata(r.Context(), actor, input)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
