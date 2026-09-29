package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// A valid 16000-rune document can exceed 64 KiB after JSON escaping. Keep
// this larger finite request budget local to knowledge commands.
func decodeKnowledgeCommand(w http.ResponseWriter, r *http.Request, command *knowledge.Command) error {
	const maxKnowledgeRequestBytes = 128 * 1024
	r.Body = http.MaxBytesReader(w, r.Body, maxKnowledgeRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(command); err != nil {
		return err
	}
	var tail any
	if err := decoder.Decode(&tail); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}
