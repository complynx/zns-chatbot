package agent

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMemoryDocumentScriptUnicodeBudget(t *testing.T) {
	t.Parallel()
	// 16k astral Unicode characters require 64k bytes before JSON envelopes.
	payload, err := json.Marshal(map[string]string{"text": strings.Repeat("🌿", 16000)})
	require.NoError(t, err)
	proposal := ScriptProposal{Code: "return input.text.length;", InputJSON: string(payload)}
	require.NoError(t, ValidateScript(proposal))
	plan, err := json.Marshal(Plan{View: KnowledgeView, ScriptAction: &proposal})
	require.NoError(t, err)
	require.LessOrEqual(t, len(plan), maxPlanBytes)
	proposal.InputJSON = `"` + strings.Repeat("x", MaxScriptInputBytes-2) + `"`
	require.NoError(t, ValidateScript(proposal))
	proposal.InputJSON = `"` + strings.Repeat("x", MaxScriptInputBytes-1) + `"`
	require.Error(t, ValidateScript(proposal))
}
