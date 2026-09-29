package bot

import (
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// Selection stores this clone beside the winning command. Resume never replaces
// it with the upload's source or the current conversation's source.
func mediaCommandSource(origin string, source *readsource.Derivation) (readsource.Derivation, error) {
	if source == nil && origin == originManual {
		return readsource.Derivation{}, nil
	}
	if source == nil || !source.Valid() {
		return readsource.Derivation{}, &core.ProblemError{Code: "invalid_derivation", Status: http.StatusBadRequest}
	}
	cloned := source.Clone()
	return cloned, nil
}
