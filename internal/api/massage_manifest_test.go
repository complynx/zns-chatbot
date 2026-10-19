package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/stretchr/testify/require"
)

func TestMassageManifestAdvertisesBoundedReadOnlyCandidateContract(t *testing.T) {
	t.Parallel()
	server := newAPI(t, &intake{})
	response := httptest.NewRecorder()
	server.InternalHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/internal/agent/manifest", nil))
	require.Equal(t, http.StatusOK, response.Code)
	var manifest struct {
		Modules []struct {
			ID      string `json:"id"`
			Methods []struct {
				ID          string      `json:"id"`
				Path        string      `json:"path"`
				Effect      string      `json:"effect"`
				InputSchema huma.Schema `json:"input_schema"`
			} `json:"methods"`
		} `json:"modules"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &manifest))
	var methods []string
	for _, module := range manifest.Modules {
		if module.ID != "zinusya_massage" {
			continue
		}
		for _, method := range module.Methods {
			methods = append(methods, method.ID)
			if method.ID == "book" || method.ID == "cancel" {
				require.Equal(t, "write", method.Effect)
			} else {
				require.Equal(t, "read", method.Effect)
			}
			require.Equal(t, "/internal/agent/massage/"+method.ID, method.Path)
			if method.ID != "candidate_starts" {
				continue
			}
			schema := method.InputSchema
			schema.PrecomputeMessages()
			valid := map[string]any{"event_id": "00000000-0000-0000-0000-000000000001",
				"expected_event_version": float64(1), "expected_version": float64(1),
				"query": map[string]any{"window_id": "night", "specialist_id": "alex", "service_id": "short",
					"from": "2026-10-25T00:00:00+03:00", "until": "2026-10-25T01:00:00+03:00", "limit": float64(2)}}
			result := &huma.ValidateResult{}
			registry := huma.NewMapRegistry("#/schemas/", huma.DefaultSchemaNamer)
			huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, valid, result)
			require.Empty(t, result.Errors)
			valid["query"].(map[string]any)["limit"] = float64(101)
			result = &huma.ValidateResult{}
			huma.Validate(registry, &schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, valid, result)
			require.NotEmpty(t, result.Errors, "Advertised schema must reject an unbounded candidate page")
		}
	}
	require.ElementsMatch(
		t,
		[]string{
			"schedule",
			"candidate_starts",
			"availability",
			"book",
			"cancel",
			"get",
			"list",
			"operation",
			"pending",
		},
		methods,
	)
}
