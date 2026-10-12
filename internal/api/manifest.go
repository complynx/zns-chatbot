package api

import "net/http"

// manifest describes the initial read-only tool. Administrative methods stay role-gated HTTP APIs.
const (
	manifestDescription = "description"
	manifestPermissions = "permissions"
	schemaObject        = "object"
	manifestType        = "type"
)

func (s *Server) manifest(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"protocol_version":  1,
		"service_id":        "shuttle",
		"revision":          "mvp-1",
		manifestDescription: "Telegram account tools",
		"caller": map[string]any{
			manifestPermissions: []string{
				"runs.create",
				"runs.control",
				"chat.read",
				"chat.events.append",
				"artifacts.read",
				"artifacts.write",
			},
			"auth": map[string]any{
				manifestType: "zitadel",
				"issuer":     s.options.Issuer,
				"subject":    s.options.ActorID,
				"audience":   s.options.LoomAudience,
			},
		},
		"modules": []any{
			map[string]any{
				"id":                "account",
				manifestDescription: "Current authenticated account",
				manifestPermissions: []string{},
				"discovery":         "static",
				"methods": []any{
					map[string]any{
						"id":                "me",
						manifestDescription: "Read the current authenticated user's account. No user ID argument is accepted.",
						manifestPermissions: []string{},
						"path":              "/internal/agent/account/me",
						"effect":            "read",
						"timeout_ms":        toolTimeoutMillis,
						"max_result_bytes":  toolResultLimit,
						"input_schema": map[string]any{
							manifestType:           schemaObject,
							"additionalProperties": false,
							"properties":           map[string]any{},
						},
						"output_schema": map[string]any{
							manifestType:           schemaObject,
							"additionalProperties": false,
							"required":             []string{"id", "telegram_user_id", "profile"},
							"properties": map[string]any{
								"id":               map[string]any{manifestType: "string", "format": "uuid"},
								"telegram_user_id": map[string]any{manifestType: "integer"},
								"profile":          map[string]any{manifestType: schemaObject},
							},
						},
						"idempotency":         false,
						"confirmation":        map[string]any{"mode": "none"},
						"artifact_mime_types": []string{},
						"external_disclosure": false,
					},
				},
			},
		},
	})
}
