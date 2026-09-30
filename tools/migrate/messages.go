package migrate

import (
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const messagesSource = "messages"
const messageRetained = "retained"
const messageOmitted = "omitted"
const messageExcluded = "excluded"
const messageOwnerExcluded = "owner_excluded"
const messageOutsideWindow = "outside_window"
const messageEventTextBytes = 5000

type MessageCandidate struct {
	TelegramID    int64           `json:"telegram_id"`
	Role          string          `json:"role"`
	Content       string          `json:"content"`
	SourceDate    json.RawMessage `json:"source_date"`
	ArchiveFields []string        `json:"archive_fields"`
}
type MessagePlanRecord struct {
	Legacy    UserLegacyReference `json:"legacy"`
	Candidate *MessageCandidate   `json:"candidate"`
	Blockers  []string            `json:"blockers"`
}

// Conversion preserves text only in the private plan. Every row still needs
// reviewed provenance, ownership and source-clock decisions before apply.
func convertMessage(record map[string]json.RawMessage) MessagePlanRecord {
	row := MessagePlanRecord{Blockers: []string{"message_review_required"}}
	candidate := MessageCandidate{ArchiveFields: []string{}}
	id, ok := telegramNumber(record["user_id"])
	if !ok || id <= 0 || id >= maxTelegramID {
		row.Blockers = append(row.Blockers, "message_owner_invalid")
		return row
	}
	candidate.TelegramID = id
	if json.Unmarshal(record["role"], &candidate.Role) != nil ||
		(candidate.Role != "user" && candidate.Role != "assistant") {
		row.Blockers = append(row.Blockers, "message_role_invalid")
		return row
	}
	if json.Unmarshal(record["content"], &candidate.Content) != nil || string(record["content"]) == "null" ||
		!utf8.ValidString(candidate.Content) ||
		strings.ContainsRune(candidate.Content, 0) {
		row.Blockers = append(row.Blockers, "message_content_invalid")
		return row
	}
	if !supportedMessageDate(record["date"]) {
		row.Blockers = append(row.Blockers, "message_clock_unresolved")
		return row
	}
	candidate.SourceDate = record["date"]
	for key := range record {
		if !slices.Contains(strings.Fields("_id user_id role content date"), key) {
			candidate.ArchiveFields = append(candidate.ArchiveFields, key)
		}
	}
	slices.Sort(candidate.ArchiveFields)
	row.Candidate = &candidate
	return row
}

func supportedMessageDate(raw json.RawMessage) bool {
	if _, err := eventInstant(raw); err == nil {
		return true
	}
	if len(raw) > 0 && raw[0] == '{' {
		fields, err := objectFields(raw, "$date")
		if err != nil {
			return false
		}
		raw = fields["$date"]
		if len(raw) > 0 && raw[0] == '{' {
			fields, err = objectFields(raw, "$numberLong")
			if err != nil {
				return false
			}
			_, err = recordID(raw)
			return err == nil && len(fields) == 1
		}
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return false
	}
	value, err := time.Parse("2006-01-02T15:04:05.999999", text)
	return err == nil && value.Format("2006-01-02T15:04:05.999999") == text
}
