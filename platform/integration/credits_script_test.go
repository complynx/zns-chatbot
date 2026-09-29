package integration_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestCreditsScriptExactAmountsAndLivePrivilege(t *testing.T) {
	t.Parallel()
	f := setup(t)
	run := func(update int64, code string, before func()) string {
		f.b.Scripts = scopeVM{before: before}
		model := &knowledgeModel{
			plans: []agent.Plan{
				{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
				{View: "workflow", Text: "Checked"},
			},
		}
		f.b.Model = model
		handle(t, f.b, message(update, 101, "Inspect credit policy"))
		require.Len(t, model.inputs, 2)
		runs := model.inputs[1].Script.Runs
		require.NotEmpty(t, runs)
		require.Empty(t, runs[len(runs)-1].Error)
		return string(runs[len(runs)-1].Result)
	}
	result := run(
		8450,
		`const own=await tools.credits.usage({});return {amount:own.policy.monthly_credits,admin:typeof tools.credits.admin !== "undefined",listed:(await tools.$list()).some(t=>t.name.startsWith("credits.admin."))};`,
		nil,
	)
	require.JSONEq(t, `{"amount":"1","admin":false,"listed":false}`, result)
	_, err := f.db.Exec(t.Context(), `INSERT INTO core.pass_booking_admins(owner) VALUES('alice')`)
	require.NoError(t, err)
	result = run(
		8451,
		`const before=await tools.credits.admin.usage({payer:"bob"});const changed=await tools.credits.admin.policy({payer:"bob",amount:"9223372036.854775807",version:before.policy.version});return changed.monthly_credits;`,
		nil,
	)
	require.JSONEq(t, `"9223372036.854775807"`, result)
	result = run(
		8452,
		`const listed=(await tools.$list()).some(t=>t.name.startsWith("credits.admin."));let denied=false;try{await tools.credits.admin.usage({payer:"bob"});}catch(_){denied=true;}return {listed,denied};`,
		func() {
			_, revokeErr := f.db.Exec(t.Context(), `DELETE FROM core.pass_booking_admins WHERE owner='alice'`)
			require.NoError(t, revokeErr)
		},
	)
	require.JSONEq(t, `{"listed":false,"denied":true}`, result)
}
