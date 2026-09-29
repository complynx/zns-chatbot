package api

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const memoryArchiveAssistant = "assistant"

type memoryArchiveInput struct {
	ExpectedGeneration *int64 `json:"expected_generation,omitempty"`
	SourceKey          string `json:"source_key"`
	Kind               string `json:"kind"`
	Text               string `json:"text"`
	ReplyToUpdateID    int64  `json:"reply_to_update_id"`
	Media              bool   `json:"media"`
}

// Trusted archival uses the same owner-bound service identity as source binding.
// Core retains sensitive-request suppression; the bot passes only host metadata.
func memoryArchiveRoutes(
	mux *http.ServeMux,
	service knowledge.Service,
	archive conversation.Service,
	signer identity.Signer,
	logger *slog.Logger,
) {
	mux.HandleFunc("POST /internal/history/archive", func(w http.ResponseWriter, r *http.Request) {
		actor, ok := memoryHostActor(w, r, signer)
		if !ok {
			return
		}
		var input memoryArchiveInput
		if Decode(w, r, &input) != nil || !validArchiveInput(input) {
			JSON(w, http.StatusBadRequest, map[string]string{codeField: invalidJSON})
			return
		}
		text, err := memoryArchiveText(r, service, actor, input)
		if err == nil {
			if input.ExpectedGeneration != nil {
				err = archive.AppendAtGeneration(
					r.Context(),
					actor,
					input.SourceKey,
					input.Kind,
					text,
					*input.ExpectedGeneration,
				)
			} else {
				err = archive.Append(r.Context(), actor, input.SourceKey, input.Kind, text)
			}
		}
		respondKnowledge(logger, w, map[string]bool{"ok": err == nil}, err)
	})
}

func validArchiveInput(input memoryArchiveInput) bool {
	const maxSourceKey = 200
	if input.ExpectedGeneration != nil && (input.Kind != memoryArchiveAssistant || *input.ExpectedGeneration < 0) {
		return false
	}
	if input.SourceKey == "" || len(input.SourceKey) > maxSourceKey || input.ReplyToUpdateID < 0 {
		return false
	}
	if input.Kind != "user" && input.Kind != memoryArchiveAssistant && input.Kind != "manual" &&
		input.Kind != "system" {
		return false
	}
	if input.Kind != memoryArchiveAssistant {
		return input.ReplyToUpdateID == 0 && !input.Media
	}
	return input.ReplyToUpdateID > 0 && input.SourceKey == "tg-assistant-"+strconv.FormatInt(input.ReplyToUpdateID, 10)
}

func memoryArchiveText(
	r *http.Request,
	service knowledge.Service,
	actor string,
	input memoryArchiveInput,
) (string, error) {
	if input.Kind != memoryArchiveAssistant {
		return input.Text, nil
	}
	var sensitive bool
	err := service.DB.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM core.conversation_events WHERE owner=$1 AND source_key=$2 AND text='[sensitive text omitted]')`, actor, "tg-user-"+strconv.FormatInt(input.ReplyToUpdateID, 10)).
		Scan(&sensitive)
	if err != nil {
		return "", err
	}
	text := input.Text
	if sensitive {
		text = "[response to sensitive request omitted]"
	}
	if input.Media {
		text = "[media response; expiring content omitted]"
	}
	return text, nil
}
