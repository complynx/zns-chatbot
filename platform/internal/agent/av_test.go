package agent_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/mediaproc"
)

func TestAVTranscriptBudgetPreservesEscapedSpeech(t *testing.T) {
	t.Parallel()
	text := strings.Repeat("\u0001", 64<<10)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input agent.Input
		if !assert.NoError(t, json.NewDecoder(r.Body).Decode(&input)) {
			return
		}
		if assert.NotNil(t, input.AV) {
			assert.Equal(t, text, input.AV.Transcript.Text)
		}
		assert.NoError(t, json.NewEncoder(w).Encode(agent.Plan{View: "workflow", Text: "Acknowledged."}))
	}))
	defer server.Close()
	model := agent.Remote{URL: server.URL}
	_, err := model.Plan(
		t.Context(),
		agent.Input{AV: &agent.AVContext{Transcript: mediaproc.Transcript{Status: "ok", Text: text}}},
	)
	require.NoError(t, err)
	_, err = model.Plan(
		t.Context(),
		agent.Input{AV: &agent.AVContext{Transcript: mediaproc.Transcript{Status: "ok", Text: text + "a"}}},
	)
	require.Error(t, err)
}
