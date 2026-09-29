package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWireOmitsUnavailableTools(t *testing.T) {
	t.Parallel()
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		for _, hidden := range []string{"proof_accept", "proof_reject", "admin_assign", "admin_uncouple", "payment_queue", "review_card", "remove_fact"} {
			assert.NotContains(t, string(body), hidden)
		}
		calls++
		response := emptyActionsPlan
		if calls == 1 {
			response = `{"skills":["registration","knowledge"],"reply_language":"en"}`
		}
		assert.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"status": "completed", "output": []any{map[string]any{
				"type": "message", "content": []any{map[string]any{"type": "output_text", "text": response}},
			}},
		}))
	}))
	defer server.Close()
	model := OpenAI{Key: "synthetic-test", BaseURL: server.URL, HTTP: server.Client()}
	_, err := model.Plan(t.Context(), capabilityTestInput("ordinary"))
	require.NoError(t, err)
	assert.Equal(t, 2, calls)
}
