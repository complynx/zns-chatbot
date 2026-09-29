package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// memoryProvenanceRoutes uses a distinct service audience. Normal user tokens
// cannot attach sources, and the signed actor cannot be replaced by JSON fields.
func memoryProvenanceRoutes(
	mux *http.ServeMux,
	service knowledge.Service,
	history conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	memoryArchiveRoutes(mux, history, signer, logger)
	memoryAssessmentRoutes(mux, service, signer, logger)
	memorySummaryRoutes(mux, history, signer, logger)
	mux.HandleFunc("POST /internal/memory/sources", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input struct {
			OperationKey string `json:"operation_key"`
			UpdateID     int64  `json:"update_id"`
		}
		const maxOperationKey = 200
		if Decode(w, r, &input) != nil || input.UpdateID <= 0 || input.OperationKey == "" ||
			len(input.OperationKey) > maxOperationKey {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		err := service.AttachMemorySources(
			r.Context(),
			actor,
			input.OperationKey,
			[]string{"tg-user-" + strconv.FormatInt(input.UpdateID, 10)},
		)
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}

func memoryHostActor(w http.ResponseWriter, r *http.Request, signer identity.Signer) (string, bool) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	actor, err := signer.VerifyMemoryProvenance(token)
	if !ok || err != nil {
		JSON(w, http.StatusUnauthorized, map[string]string{codeField: unauthorized})
		return "", false
	}
	return actor, true
}
