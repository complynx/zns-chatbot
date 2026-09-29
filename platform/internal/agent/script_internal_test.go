package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScriptProposalBoundsAndExclusiveActions(t *testing.T) {
	t.Parallel()
	good := ScriptProposal{Code: "return input.values.length;", InputJSON: `{"values":[1,2]}`}
	require.NoError(t, ValidateScript(good))
	for _, bad := range []ScriptProposal{
		{Code: "return 1;", InputJSON: "{"}, {Code: "", InputJSON: "null"},
		{Code: strings.Repeat("x", MaxScriptCodeBytes), InputJSON: "null"},
		{Code: "return input;", InputJSON: `"` + strings.Repeat("x", MaxScriptInputBytes) + `"`},
	} {
		require.Error(t, ValidateScript(bad))
	}
	require.NoError(t, Validate(Plan{View: workflowView, ScriptAction: &good}))
	require.Error(
		t,
		Validate(
			Plan{
				View:            KnowledgeView,
				ScriptAction:    &good,
				KnowledgeAction: &KnowledgeProposal{Name: "memo_set", FactKey: "food", Text: "x"},
			},
		),
	)
	require.Error(
		t,
		Validate(Plan{View: workflowView, ScriptAction: &good, Action: &Proposal{Name: "select", SlotID: "x"}}),
	)
}

func TestScriptingSkillSelectionAndStrictPlan(t *testing.T) {
	t.Parallel()
	const plan = `{"text":"","view":"workflow","action":null,"order_action":null,"profile_action":null,"media_action":null,"knowledge_action":null,"history_action":null,"registration_action":null,"script_action":{"code":"return input.a+input.b;","input_json":"{\"a\":2,\"b\":3}"}}`
	require.NoError(t, codexRequiredFields(plan))
	result, err := planWithSkills(
		t.Context(),
		Input{Text: "Add these values", Script: &ScriptContext{Available: true, Remaining: 2}},
		func(_ context.Context, p providerPrompt) (string, error) {
			if p.name == selectionName {
				assert.NotContains(t, p.instructions, "function body")
				return `{"skills":["scripting"],"reply_language":"en"}`, nil
			}
			assert.Contains(t, p.instructions, "function body")
			assert.NotContains(t, p.instructions, "Memos belong only to this actor")
			return plan, nil
		},
	)
	require.NoError(t, err)
	require.NotNil(t, result.ScriptAction)
	for _, bad := range []string{
		strings.Replace(plan, `"code":"return input.a+input.b;"`, `"code":"return 1;","code":"return 2;"`, 1),
		strings.Replace(plan, `"input_json":`, `"Input_JSON":`, 1),
		strings.Replace(plan, `"code":`, `"actor":"admin","code":`, 1),
	} {
		_, err = decodePlan(bad, nil)
		require.Error(t, err)
	}
}
