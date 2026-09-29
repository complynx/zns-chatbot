package integration_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/passes"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

func TestScriptProfileReadsOwnerScopeAndBounds(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			// HTML-sensitive characters expand sixfold in JSON while remaining valid profile text.
			text := strings.Repeat("<", 300)
			_, err := f.db.Exec(
				t.Context(),
				`UPDATE core.users SET language=$1, can_book=false WHERE id='alice'`,
				language,
			)
			require.NoError(t, err)
			_, err = f.db.Exec(
				t.Context(),
				`INSERT INTO core.pass_profiles(owner,legal_name,passport) VALUES('alice',$1,$1),('bob','Foreign name','Foreign passport')`,
				text,
			)
			require.NoError(t, err)
			executions := 0
			f.b.Scripts = hostScriptFunc(
				func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
					executions++
					require.Len(t, tools, 55)
					require.NoError(t, scriptprotocol.ValidateTools(tools))
					list := scriptCall(ctx, t, callback, "$list", "{}")
					assert.Contains(t, string(list), "preferences.get")
					assert.Contains(t, string(list), "profile.get")
					assert.Contains(t, string(list), "history.read")
					assert.NotContains(t, string(list), "workflow.select")
					for _, name := range []string{"preferences.get", "profile.get"} {
						help := scriptCall(ctx, t, callback, "$help", `{"name":"`+name+`"}`)
						assert.Contains(t, string(help), `"additionalProperties":false`)
						_, callErr := callback(
							ctx,
							scriptclient.ToolCall{Name: name, Arguments: json.RawMessage(`{"owner":"bob"}`)},
						)
						require.Error(t, callErr)
					}
					preferences := scriptCall(ctx, t, callback, "preferences.get", "{}")
					assert.JSONEq(t, `{"language":"`+language+`"}`, string(preferences))
					profile := scriptCall(ctx, t, callback, "profile.get", "{}")
					assert.Greater(t, len(profile), 2048)
					assert.Less(t, len(profile), 8*1024)
					var value passes.Profile
					require.NoError(t, json.Unmarshal(profile, &value))
					assert.Equal(t, "alice", value.Owner)
					assert.Equal(t, text, value.LegalName)
					assert.Equal(t, text, value.Passport)
					assert.NotContains(t, string(profile), "Foreign")
					return json.RawMessage(`{"read":true}`), nil
				},
			)
			model := &knowledgeModel{plans: []agent.Plan{
				{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
				{View: agent.OrdersView, Text: "Read completed"},
			}}
			f.b.Model = model
			update := message(1988, identity.AliceTelegramID, "Read my profile and language")
			handle(t, f.b, update)
			handle(t, f.b, update)
			assert.Equal(t, 1, executions)
			require.Len(t, model.inputs, 2)
			require.Len(t, model.inputs[1].Script.Runs[0].Calls, 2)
			for _, call := range model.inputs[1].Script.Runs[0].Calls {
				assert.Empty(t, call.Error)
			}
		})
	}
}
