package agent

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/api"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func TestModelSettingsProviderIsolation(t *testing.T) {
	t.Parallel()
	for _, selection := range []modelsettings.Selection{{Model: "gpt-6-sol", Effort: "high"}, {Model: "gpt-6-astra", Effort: "low"}, {Model: modelsettings.DefaultModel}} {
		t.Run(selection.Model, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload struct {
					Model     string `json:"model"`
					Reasoning struct {
						Effort string `json:"effort"`
					} `json:"reasoning"`
				}
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
				assert.Equal(t, selection.Model, payload.Model)
				assert.Equal(t, selection.Effort, payload.Reasoning.Effort)
				api.JSON(
					w,
					http.StatusOK,
					map[string]any{
						"status": "completed",
						"output": []any{
							map[string]any{
								"type":    "message",
								"content": []any{map[string]string{"type": "output_text", "text": "{}"}},
							},
						},
					},
				)
			}))
			defer server.Close()
			ctx := modelsettings.WithSelection(t.Context(), selection)
			provider := OpenAI{Key: "synthetic", BaseURL: server.URL, HTTP: server.Client()}
			for _, name := range []string{selectionName, "zns_action_plan"} {
				_, err := provider.structured(ctx, providerPrompt{name: name, schema: "{}", input: []byte("{}")})
				require.NoError(t, err)
			}
			args := codexModelArguments(ctx, "temporary", "schema")
			assert.Contains(t, args, selection.Model)
			if selection.Effort != "" {
				assert.Contains(t, args, "model_reasoning_effort="+strconv.Quote(selection.Effort))
			}
		})
	}
}
func TestModelSettingsPrivateProtocolValidation(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodPost, "/plan", nil)
	request.Header.Set(modelHeader, "--unsafe")
	_, err := modelRequestContext(request)
	require.Error(t, err)
	request.Header.Set(modelHeader, "gpt-6-luna")
	request.Header.Set(effortHeader, "ultra")
	_, err = modelRequestContext(request)
	require.Error(t, err)
}
