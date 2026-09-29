package agent_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestRemoteRejectsAmbiguousPlan(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"trailing":         `{"text":"ok","view":"workflow"} {}`,
		"duplicate":        `{"text":"first","text":"second","view":"workflow"}`,
		"case_alias":       `{"Text":"ok","view":"workflow"}`,
		"nested_duplicate": `{"text":"ok","view":"profile","profile_action":{"name":"set","field":"legal_name","value":"A B","value":"C D"}}`,
		"nested_alias":     `{"text":"ok","view":"profile","profile_action":{"Name":"set","field":"legal_name","value":"A B"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, err := w.Write([]byte(body))
				assert.NoError(t, err)
			}))
			defer server.Close()
			_, err := (agent.Remote{URL: server.URL}).Plan(t.Context(), agent.Input{})
			require.Error(t, err)
		})
	}
}
