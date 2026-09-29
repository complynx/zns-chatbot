package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/readsource"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func memoryAuthorityRoute(
	mux *http.ServeMux,
	archive conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST /internal/history/authority", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input struct {
			ReadAuthorities []readsource.Authority `json:"read_authorities"`
		}
		if Decode(w, r, &input) != nil || !readsource.Valid(input.ReadAuthorities) {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := archive.CheckReadAuthorities(r.Context(), actor, input.ReadAuthorities)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
