package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestScriptModelPermissionsAndReplay(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"own", "others", "global"} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			f := passMenuFixture(t)
			service := modelsettings.Service{DB: f.db}
			require.NoError(
				t,
				service.Grant(
					t.Context(),
					"bob",
					modelsettings.Grant{Owner: "alice", Capability: scope, Enabled: true},
				),
			)
			executions := 0
			f.b.Scripts = hostScriptFunc(
				func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
					executions++
					list := scriptCall(ctx, t, callback, "$list", `{}`)
					assert.Contains(t, string(list), "models."+scope+".set")
					assert.NotContains(t, string(list), "models.grants.set")
					for _, hidden := range []string{"own", "others", "global"} {
						if hidden != scope {
							assert.NotContains(t, string(list), "models."+hidden+".set")
						}
					}
					target := ""
					if scope == "others" {
						target = `"owner":"visitor",`
					}
					help := scriptCall(ctx, t, callback, "$help", `{"name":"models.`+scope+`.set"}`)
					assert.NotContains(t, string(help), "operation_key")
					args := `{` + target + `"model":"gpt-6-sol","effort":"high"}`
					result := scriptCall(ctx, t, callback, "models."+scope+".set", args)
					var state modelsettings.State
					require.NoError(t, json.Unmarshal(result, &state))
					assert.EqualValues(t, 1, state.Version)
					assert.Equal(t, "gpt-6-sol", state.Model)
					require.NoError(
						t,
						service.Grant(
							ctx,
							"bob",
							modelsettings.Grant{Owner: "alice", Capability: scope, Enabled: false},
						),
					)
					_, err := callback(
						ctx,
						scriptclient.ToolCall{Name: "models." + scope + ".set", Arguments: json.RawMessage(args)},
					)
					require.Error(t, err)
					_, err = callback(
						ctx,
						scriptclient.ToolCall{
							Name:      "$help",
							Arguments: json.RawMessage(`{"name":"models.` + scope + `.set"}`),
						},
					)
					require.Error(t, err)
					return json.RawMessage(`{"changed":true}`), nil
				},
			)
			f.b.Model = &knowledgeModel{plans: []agent.Plan{
				{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
				{View: "workflow", Text: "Model updated"},
			}}
			update := message(8700, 101, "Select the model")
			handle(t, f.b, update)
			handle(t, f.b, update)
			assert.Equal(t, 1, executions)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.model_setting_operations WHERE actor='alice'`).
					Scan(&count),
			)
			assert.Equal(t, 1, count)
		})
	}
}

func TestScriptModelGrantAndOrdinaryVisibility(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			result := scriptCall(
				ctx,
				t,
				callback,
				"models.grants.set",
				`{"owner":"alice","capability":"own","enabled":true}`,
			)
			assert.JSONEq(t, `{"ok":true}`, string(result))
			_, err := f.db.Exec(ctx, `DELETE FROM core.pass_booking_admins WHERE owner='bob'`)
			require.NoError(t, err)
			list := scriptCall(ctx, t, callback, "$list", `{}`)
			assert.Contains(t, string(list), "models.effective")
			assert.NotContains(t, string(list), "models.grants")
			assert.NotContains(t, string(list), "models.global")
			_, err = callback(
				ctx,
				scriptclient.ToolCall{
					Name:      "models.grants.set",
					Arguments: json.RawMessage(`{"owner":"visitor","capability":"global","enabled":true}`),
				},
			)
			require.Error(t, err)
			return json.RawMessage(`{"done":true}`), nil
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: "workflow", Text: "Granted"},
	}}
	handle(t, f.b, message(8701, 202, "Allow Alice to choose her model"))
	permissions, err := (modelsettings.Service{DB: f.db}).Permissions(t.Context(), "alice")
	require.NoError(t, err)
	assert.True(t, permissions["own"])
	assert.False(t, permissions["global"])
}
