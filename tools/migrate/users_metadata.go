package migrate

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
	"unicode/utf8"
)

const metadataUsernameLimit = 64
const metadataNameLimit = 256
const metadataPrintLimit = 513

func mapUserMetadata(record map[string]json.RawMessage, result *UserPlanRecord) {
	candidate := result.Candidate
	candidate.TelegramMetadataUpdate = -1
	candidate.Username = metadataValue(record, userUsernameField, metadataUsernameLimit, true, result)
	candidate.FirstName = metadataValue(record, "first_name", metadataNameLimit, false, result)
	candidate.LastName = metadataValue(record, "last_name", metadataNameLimit, true, result)
	candidate.PrintName = metadataValue(record, "print_name", metadataPrintLimit, false, result)
}

func metadataValue(
	record map[string]json.RawMessage,
	field string,
	limit int,
	nullable bool,
	result *UserPlanRecord,
) *string {
	raw, present := record[field]
	value := ""
	disposition := "mapped"
	switch {
	case !present:
		disposition = "default_absent"
	case nullable && bytes.Equal(raw, []byte("null")):
		disposition = "default_null"
	case bytes.Equal(raw, []byte("null")) || json.Unmarshal(raw, &value) != nil || !validMetadataValue(value, field, limit):
		disposition = "invalid_value"
		addUserBlocker(result, "invalid_mapped_field")
	}
	result.Fields = append(result.Fields, UserFieldDisposition{Field: field, Disposition: disposition})
	if disposition == "invalid_value" {
		return nil
	}
	return &value
}

func validMetadataValue(value, field string, limit int) bool {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	return !strings.ContainsFunc(value, func(char rune) bool {
		if field == userUsernameField {
			return (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '_'
		}
		// Reject replacement characters too: JSON decoding replaces invalid surrogates.
		return unicode.IsControl(char) || char == '\ufffe' || char == '\uffff' || char == utf8.RuneError
	})
}
