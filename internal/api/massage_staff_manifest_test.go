package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestMassageStaffManifestValidatesBoundedTimetableCursor(t *testing.T) {
	t.Parallel()
	server := newAPI(t, &intake{})
	response := httptest.NewRecorder()
	server.InternalHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/agent/manifest", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID                         string      `json:"id"`
				Path                       string      `json:"path"`
				Effect                     string      `json:"effect"`
				InputSchema                huma.Schema `json:"input_schema"`
				Idempotency                bool        `json:"idempotency"`
				StatusPath                 string      `json:"status_path"`
				DeduplicationWindowSeconds int64       `json:"deduplication_window_seconds"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &manifest))
	var found []string
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_massage_staff" {
			continue
		}
		for _, method := range module.Methods {
			found = append(found, method.ID)
			switch method.ID {
			case "set_preferences":
				require.Equal(t, "write", method.Effect)
				require.True(t, method.Idempotency)
				require.Equal(t, "/internal/agent/massage_staff/set_preferences/status", method.StatusPath)
				require.GreaterOrEqual(t, method.DeduplicationWindowSeconds, int64(86400))
				schema := method.InputSchema
				schema.PrecomputeMessages()
				registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
				input := map[string]any{
					"event_id":         "00000000-0000-0000-0000-000000000001",
					"expected_version": float64(0),
					"notify_bookings":  false,
				}
				result := &huma.ValidateResult{}
				huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
				require.Empty(t, result.Errors, "Explicit false is a valid preference")
				delete(input, "notify_bookings")
				result = &huma.ValidateResult{}
				huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
				require.NotEmpty(t, result.Errors, "A missing boolean must not mean false")
				input["notify_next"] = false
				result = &huma.ValidateResult{}
				huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
				require.Empty(t, result.Errors, "Either explicit preference is valid without changing the other")

			case "instant":
				require.Equal(t, "write", method.Effect)
				require.True(t, method.Idempotency)
				require.Equal(t, "/internal/agent/massage_staff/instant/status", method.StatusPath)
				require.GreaterOrEqual(t, method.DeduplicationWindowSeconds, int64(86400))
			default:
				require.Equal(t, "read", method.Effect)
			}
			require.Equal(t, "/internal/agent/massage_staff/"+method.ID, method.Path)
			if method.ID != "timetable" {
				continue
			}
			schema := method.InputSchema
			schema.PrecomputeMessages()
			registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
			input := map[string]any{
				"event_id":  "00000000-0000-0000-0000-000000000001",
				"window_id": "night",
				"from":      "2026-10-25T00:00:00+02:00",
				"until":     "2026-10-25T03:00:00+01:00",
				"own_only":  true,
				"limit":     float64(20),
			}
			result := &huma.ValidateResult{}
			huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
			require.Empty(t, result.Errors)
			input["limit"] = float64(21)
			result = &huma.ValidateResult{}
			huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
			require.NotEmpty(t, result.Errors)
			input["limit"] = float64(1)
			input["after"] = map[string]any{
				"id":        "00000000-0000-0000-0000-000000000002",
				"starts_at": "2026-10-25T01:00:00+02:00",
			}
			result = &huma.ValidateResult{}
			huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
			require.NotEmpty(t, result.Errors, "A cursor must retain event/window/range/ownership fields")
		}
	}
	require.ElementsMatch(
		t,
		[]string{"events", "schedule", "windows", "timetable", "client", "preferences", "set_preferences", "instant"},
		found,
	)
}
