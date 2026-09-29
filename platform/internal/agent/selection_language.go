package agent

import (
	"encoding/json"
	"errors"

	"golang.org/x/text/language"
)

func selectedReplyLanguage(raw string) ([]skillID, string, error) {
	const maxSelectionBytes = 512
	if len(raw) > maxSelectionBytes {
		return nil, "", errors.New("skill selection exceeds budget")
	}
	fields, err := codexObject([]byte(raw))
	if err != nil || len(fields) != 2 {
		return nil, "", errors.New("invalid skill and language selection")
	}
	var tag string
	if json.Unmarshal(fields["reply_language"], &tag) != nil || len(tag) > 35 || tag == "" {
		return nil, "", errors.New("invalid reply language")
	}
	parsed, err := language.Parse(tag)
	if err != nil || parsed == language.Und {
		return nil, "", errors.New("invalid reply language")
	}
	ids, err := decodeSkills(`{"skills":` + string(fields["skills"]) + `}`)
	return ids, parsed.String(), err
}
