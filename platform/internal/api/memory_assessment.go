package api

import (
	"log/slog"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func memoryAssessmentRoutes(
	mux *http.ServeMux,
	service knowledge.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST /internal/memory/assess", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input struct {
			Key        string `json:"key"`
			ProposalID int64  `json:"proposal_id"`
			Version    int64  `json:"version"`
			Worthwhile bool   `json:"worthwhile"`
			Reason     string `json:"reason"`
		}
		if Decode(w, r, &input) != nil {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		result, err := service.Assess(
			r.Context(),
			actor,
			knowledge.Assessment{
				Key:        input.Key,
				ProposalID: input.ProposalID,
				Version:    input.Version,
				Worthwhile: input.Worthwhile,
				Reason:     input.Reason,
			},
		)
		respondKnowledge(logger, w, result, err)
	})
}
