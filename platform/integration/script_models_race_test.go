package integration_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

type modelInterleavingTransport struct {
	before func()
	done   bool
}

func (transport *modelInterleavingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost && request.URL.Path == "/v1/model-settings" && !transport.done {
		transport.done = true
		transport.before()
	}
	return http.DefaultTransport.RoundTrip(request)
}

func TestScriptModelPostBindingAuthorityAndVersion(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"revoke", "concurrent-setting"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service := modelsettings.Service{DB: f.db}
			require.NoError(
				t,
				service.Grant(
					t.Context(),
					"bob",
					modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: true},
				),
			)
			transport := &modelInterleavingTransport{before: func() {
				if change == "revoke" {
					require.NoError(
						t,
						service.Grant(
							t.Context(),
							"bob",
							modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: false},
						),
					)
					return
				}
				_, err := service.Set(
					t.Context(),
					"bob",
					"alice",
					modelsettings.Change{Model: "gpt-6-astra", Effort: "low", OperationKey: "manual-race", Version: 0},
				)
				require.NoError(t, err)
			}}
			f.b.API.HTTP = &http.Client{Transport: transport}
			f.b.Scripts = scopeVM{}
			model := &knowledgeModel{plans: []agent.Plan{
				{View: "workflow", ScriptAction: &agent.ScriptProposal{
					Code: `return tools.models.own.set({model:"gpt-6-sol",effort:"high"});`, InputJSON: "null",
				}},
				{View: "workflow", Text: "State changed"},
			}}
			f.b.Model = model
			handle(t, f.b, message(8702, 101, "Change model"))
			require.Len(t, model.inputs, 2)
			run := model.inputs[1].Script.Runs[0]
			require.Len(t, run.Calls, 1)
			if change == "concurrent-setting" {
				assert.Empty(t, run.Error)
				assert.JSONEq(t, `{"error":"stale","restart":true}`, string(run.Result))
				assert.Equal(t, "stale", run.Calls[0].Error)
			} else {
				assert.NotEmpty(t, run.Error)
				assert.Equal(t, "denied", run.Calls[0].Error)
			}
			assert.True(t, transport.done)
			selection, err := service.Effective(t.Context(), "alice")
			require.NoError(t, err)
			assert.NotEqual(t, "gpt-6-sol", selection.Model)
			if change == "concurrent-setting" {
				assert.Equal(t, "gpt-6-astra", selection.Model)
			}
		})
	}
}
