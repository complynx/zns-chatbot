package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func derivedKnowledgeRoutes(mux *http.ServeMux, service knowledge.Service,
	authorizer applicationauth.Authorizer, signer identity.Signer, logger *slog.Logger,
) {
	mux.Handle("POST /internal/knowledge/derived", authenticatedDerivation(authorizer, signer,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var command knowledge.Command
			source, err := decodeDerivedMutation(w, r, &command, maxKnowledgeRequestBytes)
			if err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			value, err := service.ExecuteDerived(r.Context(), requestOwner(r), command, source)
			respondKnowledge(logger, w, value, err)
		}), logger))
}
