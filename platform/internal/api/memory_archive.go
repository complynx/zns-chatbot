package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

// Archive operations are host-only. Distinct requests prevent omitted model
// provenance from silently selecting an original or trusted write.
func memoryArchiveRoutes(
	mux *http.ServeMux,
	archive conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	memoryAuthorityRoute(mux, archive, signer, logger)
	originalArchiveRoute(mux, archive, signer, logger)
	derivedArchiveRoute(mux, archive, signer, logger)
	outcomeArchiveRoute(mux, archive, signer, logger)
}

func originalArchiveRoute(
	mux *http.ServeMux,
	archive conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST /internal/history/archive/original", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input conversation.OriginalArchive
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := archive.ArchiveOriginal(r.Context(), actor, input.SourceKey, input.Kind, input.Text)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}

func derivedArchiveRoute(
	mux *http.ServeMux,
	archive conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST /internal/history/archive/derived", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input conversation.DerivedArchive
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := archive.ArchiveDerived(r.Context(), actor, input)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}

func outcomeArchiveRoute(
	mux *http.ServeMux,
	archive conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST /internal/history/archive/outcome", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input conversation.OutcomeArchive
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := archive.ArchiveOutcome(r.Context(), actor, input.SourceKey, input.Text)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}
