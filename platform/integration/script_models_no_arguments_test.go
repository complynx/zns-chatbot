package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
)

func TestScriptModelsNoArgumentsThroughSobekAndHost(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := modelsettings.Service{DB: f.db}
	require.NoError(
		t,
		service.Grant(t.Context(), "bob", modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: true}),
	)
	f.b.Scripts = scopeVM{}
	const code = `const effective = tools.models.effective();
const preference = await tools.preferences.get();
const own = tools.models.own.get();
const help = tools.models.own.get.$help();
let requiredDenied = false, unknownDenied = false;
try { tools.models.own.set(); } catch (_) { requiredDenied = true; }
try { tools.models.effective({owner:"bob"}); } catch (_) { unknownDenied = true; }
return {model:effective.model,language:preference.language,version:own.version,
 help:help.name,requiredDenied,unknownDenied,globalHidden:typeof tools.models.global === "undefined"};`
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: "workflow", Text: "Checked"},
	}}
	f.b.Model = model
	handle(t, f.b, message(8950, identity.AliceTelegramID, "Show my model and language"))
	require.Len(t, model.inputs, 2)
	run := model.inputs[1].Script.Runs[0]
	assert.Empty(t, run.Error)
	assert.JSONEq(
		t,
		`{"model":"gpt-6-luna","language":"en","version":0,"help":"models.own.get","requiredDenied":true,"unknownDenied":true,"globalHidden":true}`,
		string(run.Result),
	)
	var writes int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.model_setting_operations WHERE actor='alice'`).
			Scan(&writes),
	)
	assert.Zero(t, writes)
}

func TestScriptModelsNoArgumentsStillRecheckRevocation(t *testing.T) {
	t.Parallel()
	f := passMenuFixture(t)
	service := modelsettings.Service{DB: f.db}
	grant := modelsettings.Grant{Owner: "alice", Capability: "own", Enabled: true}
	require.NoError(t, service.Grant(t.Context(), "bob", grant))
	f.b.Scripts = scopeVM{before: func() {
		grant.Enabled = false
		require.NoError(t, service.Grant(t.Context(), "bob", grant))
	}}
	const code = `let denied=false, helpDenied=false;
try { tools.models.own.get(); } catch (_) { denied=true; }
try { tools.models.own.get.$help(); } catch (_) { helpDenied=true; }
return {denied,helpDenied,hidden:!tools.$list().some(t=>t.name==="models.own.get")};`
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: "workflow", Text: "Checked"},
	}}
	f.b.Model = model
	handle(t, f.b, message(8951, identity.AliceTelegramID, "Read my model settings"))
	require.Len(t, model.inputs, 2)
	run := model.inputs[1].Script.Runs[0]
	assert.Empty(t, run.Error)
	assert.True(t, json.Valid(run.Result))
	assert.JSONEq(t, `{"denied":true,"helpDenied":true,"hidden":true}`, string(run.Result))
}
