package legacyimport

import (
	"encoding/json"
	"strings"
)

// ProjectionVersion identifies the whitelist used for historical owner data.
const ProjectionVersion = "zinusya-legacy-history/v1"

const (
	memoFieldMaxBytes = 256
	projectionKind    = "kind"
)

// Record identifies one historical source object. Raw bytes remain in its source
// archive. SHA256 covers the existing inspector's canonical JSON, not a projection.
// A nil Projection means the object is retained for provenance but has no supported
// owner-facing representation. It must never be replaced by raw source JSON.
type Record struct {
	SourceInstance string
	Collection     string
	Role           string
	SourceKey      string
	OwnerID        int64
	Line           int
	SHA256         [32]byte
	Projection     json.RawMessage
}

// projectRecord permits only documented historical data. In particular, imported
// system/tool roles, response metadata and old IAM bindings are not instructions
// or authority for the current agent. Embedded pass keys require a later explicit
// source mapping; arbitrary objects in a user profile are not guessed to be passes.
func projectRecord(role string, object map[string]any) json.RawMessage {
	switch role {
	case roleMessages:
		return projectMessage(object)
	case roleUsers:
		return projectMemo("profile", object, "first_name", "last_name", "username", "print_name", "language_code")
	case rolePasses:
		memo := memoFields(object, "pass_key", "state", "type", "role")
		if paid, ok := object["paid"].(bool); ok {
			memo["paid_claim"] = paid
		}
		if len(memo) == 0 {
			return nil
		}
		return marshalProjection(map[string]any{projectionKind: "registration_claim", "fields": memo})
	case roleFiles:
		// File references and credentials are not historical content for the agent.
		return nil
	default:
		return nil
	}
}

func projectMessage(object map[string]any) json.RawMessage {
	role, ok := object["role"].(string)
	if !ok || (role != "user" && role != "assistant") {
		return nil
	}
	content, ok := object["content"].(string)
	if !ok || strings.ContainsRune(content, 0) {
		return nil
	}
	return marshalProjection(map[string]any{projectionKind: "message", "role": role, "content": content,
		"recorded_date": object["date"], "date_policy": "preserve_source"})
}

func projectMemo(kind string, object map[string]any, fields ...string) json.RawMessage {
	memo := memoFields(object, fields...)
	if len(memo) == 0 {
		return nil
	}
	return marshalProjection(map[string]any{projectionKind: kind, "fields": memo})
}

func memoFields(object map[string]any, fields ...string) map[string]any {
	memo := map[string]any{}
	for _, key := range fields {
		value, ok := object[key].(string)
		if ok && value != "" && len(value) <= memoFieldMaxBytes && !strings.ContainsRune(value, 0) {
			memo[key] = value
		}
	}
	return memo
}

func marshalProjection(value map[string]any) json.RawMessage {
	raw, _ := json.Marshal(value) // Values are validated strings, booleans and source dates.
	return raw
}
