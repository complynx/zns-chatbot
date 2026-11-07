package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestMassageInstantManifestRejectsCallerTimeAndOwner(t *testing.T) {
	t.Parallel()
	s := newAPI(t, &intake{})
	w := httptest.NewRecorder()
	s.InternalHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/agent/manifest", nil))
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID           string      `json:"id"`
				InputSchema  huma.Schema `json:"input_schema"`
				Confirmation struct {
					Mode        string `json:"mode"`
					PreparePath string `json:"prepare_path"`
				} `json:"confirmation"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &manifest))
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_massage_staff" {
			continue
		}
		for _, method := range module.Methods {
			if method.ID != "instant" {
				continue
			}
			require.Equal(t, "exact_parameters", method.Confirmation.Mode)
			require.Equal(t, "/internal/agent/massage_staff/instant/prepare", method.Confirmation.PreparePath)
			schema := method.InputSchema
			schema.PrecomputeMessages()
			registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
			input := map[string]any{
				"event_id":               "00000000-0000-0000-0000-000000000001",
				"expected_event_version": float64(1), "expected_schedule_version": float64(1),
				"selection": map[string]any{"window_id": "night", "specialist_id": "alex", "service_id": "short"},
			}
			validate := func() []error {
				result := &huma.ValidateResult{}
				huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, input, result)
				return result.Errors
			}
			require.Empty(t, validate())
			selection, ok := input["selection"].(map[string]any)
			require.True(t, ok)
			selection["starts_at"] = "2026-10-25T02:00:00+01:00"
			require.NotEmpty(t, validate(), "The server, not the caller, chooses the start")
			delete(selection, "starts_at")
			for _, key := range []string{"user_id", "staff_binding_version", "purpose"} {
				input[key] = "caller-supplied"
				require.NotEmpty(t, validate())
				delete(input, key)
			}
			return
		}
	}
	t.Fatal("Instant specialist tool missing")
}
