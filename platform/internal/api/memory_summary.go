package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func memorySummaryRoutes(
	mux *http.ServeMux,
	history conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("GET /internal/history/summary-batch", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		before, err := historyInteger(r, "before", 0)
		if err != nil || before < 0 {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		events, err := history.SummaryBatch(r.Context(), actor, before)
		respondKnowledge(logger, w, events, err)
	})
	mux.HandleFunc("POST /internal/history/summary", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input struct {
			Version int64   `json:"version"`
			IDs     []int64 `json:"ids"`
			Text    string  `json:"text"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := history.CommitSummary(r.Context(), actor, input.Version, input.IDs, input.Text)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
