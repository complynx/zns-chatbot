package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/applicationauth"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func memoryReadStateRoutes(mux *http.ServeMux, service agenthost.MemoryReadStore,
	authorizer applicationauth.Authorizer, signer identity.Signer, logger *slog.Logger,
) {
	for _, action := range []string{"reserve", "complete", "reconcile"} {
		mux.Handle("POST /internal/memory/read-state/"+action, authenticatedDerivation(authorizer, signer,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var input appclient.MemoryReadStateRequest
				if err := decodeDerivedJSON(http.MaxBytesReader(w, r.Body, appclient.MaxAPIBytes), &input); err != nil {
					JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
					return
				}
				owner := requestOwner(r)
				switch action {
				case "reserve":
					value, err := service.ReserveKnowledge(r.Context(), owner, input.UpdateID, input.Request)
					respond(logger, w, value, err)
				case "complete":
					value, err := service.CompleteKnowledge(
						r.Context(),
						owner,
						input.UpdateID,
						input.Index,
						input.Result,
					)
					respond(logger, w, value, err)
				case "reconcile":
					err := service.ReconcileMemory(r.Context(), owner, knowledge.MemoryDeletionState{})
					respond(logger, w, struct{}{}, err)
				}
			}), logger))
	}
}
