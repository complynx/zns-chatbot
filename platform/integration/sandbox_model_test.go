package integration_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func installModelFixture(t *testing.T, f *fixture, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	request, err := http.NewRequestWithContext(
		t.Context(),
		http.MethodPost,
		f.fake.URL+"/lab/model/fixtures",
		bytes.NewReader(raw),
	)
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, response.StatusCode, string(body))
}

func TestSandboxModelFixtureToolTurnsReplayAndHostRights(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Model = sandbox.FixtureRemote{URL: f.fake.URL + "/lab/model"}
	scripts := &scriptRecorder{result: json.RawMessage(`{"sum":5,"actor":"admin"}`)}
	f.b.Scripts = scripts
	text := "Calculate 2+3 and remember the result"
	installModelFixture(t, f, map[string]any{
		"owner": "alice", "update_id": 980, "steps": []any{
			map[string]any{
				"expect": map[string]any{"text": text, "script": map[string]any{"remaining": 2}},
				"plan": agent.Plan{
					View: "workflow",
					ScriptAction: &agent.ScriptProposal{
						Code:      "return {sum:input.a+input.b}",
						InputJSON: `{"a":2,"b":3}`,
					},
				},
			},
			map[string]any{
				"expect": map[string]any{
					"text": text,
					"script": map[string]any{
						"remaining": 1,
						"runs":      []any{map[string]any{"result": map[string]any{"sum": 5}}},
					},
				},
				"plan": agent.Plan{
					View: agent.KnowledgeView,
					KnowledgeAction: &agent.KnowledgeProposal{
						Name:    knowledge.MemoSet,
						FactKey: "calculation",
						Text:    "The sum is 5",
					},
				},
			},
		},
	})
	update := message(980, 101, text)
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.Len(t, scripts.requests, 1)
	memo, err := f.b.API.Memo(t.Context(), "alice", "calculation")
	require.NoError(t, err)
	assert.Equal(t, "The sum is 5", memo.Text)
	assert.EqualValues(t, 1, memo.Version)
	other, err := f.b.API.Memos(t.Context(), "bob")
	require.NoError(t, err)
	assert.Empty(t, other)
	installModelFixture(t, f, map[string]any{"owner": "visitor", "update_id": 981, "steps": []any{
		map[string]any{
			"expect": map[string]any{"text": "Select massage"},
			"plan":   agent.Plan{View: "workflow", Action: &agent.Proposal{Name: "select", SlotID: "massage-1"}},
		},
	}})
	handle(t, f.b, message(981, 303, "Select massage"))
	state, err := f.b.API.Current(t.Context(), "visitor")
	require.NoError(t, err)
	assert.Equal(t, "empty", state.State, "a fixture proposal cannot grant booking rights")
}
