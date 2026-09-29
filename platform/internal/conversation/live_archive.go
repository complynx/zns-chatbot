package conversation

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// MaxLiveArchiveBytes preserves the host HTTP request budget, including the
// complete canonical JSON envelope and escaping. Host requests use Marshal,
// without a trailing newline. Imported originals retain MaxBodyBytes instead.
const MaxLiveArchiveBytes = 65536

type OriginalArchive struct {
	SourceKey string `json:"source_key"`
	Kind      string `json:"kind"`
	Text      string `json:"text"`
}

type OutcomeArchive struct {
	SourceKey string `json:"source_key"`
	Text      string `json:"text"`
}

func validLiveArchive(input any) bool {
	encoded, err := json.Marshal(input)
	return err == nil && len(encoded) <= MaxLiveArchiveBytes
}

// Validate applies live admission before transport sends or database access.
func (input OriginalArchive) Validate() error {
	if !validHostArchive(input.SourceKey, input.Text) || (input.Kind != "user" && input.Kind != "manual") ||
		!validLiveArchive(input) {
		return invalidHostArchive()
	}
	return nil
}

func (input OutcomeArchive) Validate() error {
	if !validHostArchive(input.SourceKey, input.Text) || !validLiveArchive(input) {
		return invalidHostArchive()
	}
	return nil
}

func (input DerivedArchive) Validate() error {
	if !validHostArchive(input.SourceKey, input.Text) || input.ExpectedGeneration == nil ||
		*input.ExpectedGeneration < 0 || input.ReadAuthorities == nil || !readsource.Valid(input.ReadAuthorities) ||
		input.ReplyToUpdateID <= 0 || input.SourceKey != "tg-assistant-"+strconv.FormatInt(input.ReplyToUpdateID, 10) ||
		!validLiveArchive(input) {
		return invalidHostArchive()
	}
	return nil
}
func validHostArchive(key, text string) bool {
	const maxSourceKey = 200
	return key != "" && len(key) <= maxSourceKey && utf8.ValidString(key) && !strings.ContainsRune(key, 0) &&
		len(text) <= MaxLiveArchiveBytes && utf8.ValidString(text)
}

func invalidHostArchive() error {
	return &core.ProblemError{Status: http.StatusBadRequest, Code: "invalid_json"}
}
