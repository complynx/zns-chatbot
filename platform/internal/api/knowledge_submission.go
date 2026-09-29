package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// Manual host submission is deliberately absent from the user/model command API.
func knowledgeSubmissionRoutes(
	mux *http.ServeMux,
	service knowledge.Service,
	authorizer applicationauth.Authorizer,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.Handle(
		"POST /internal/knowledge/submit",
		authenticatedDerivation(authorizer, signer, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var input knowledge.Submission
			if err := decodeDerivedJSON(http.MaxBytesReader(w, r.Body, maxKnowledgeRequestBytes), &input); err != nil {
				JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
				return
			}
			result, err := service.SubmitProposal(r.Context(), requestOwner(r), input)
			respondKnowledge(logger, w, result, err)
		}), logger),
	)
}
