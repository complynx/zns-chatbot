package bot

import (
	"encoding/json"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/interaction"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	knowledgeauthority "github.com/complynx/zns-chatbot/platform/internal/knowledge/authority"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestDeferredKnowledgeResultKeepsAuthoritiesHostOnly(t *testing.T) {
	t.Parallel()
	refs := []readsource.Authority{
		{Knowledge: knowledgeauthority.ReadAuthority{Kind: knowledgeauthority.Review, Scope: "host-only-review-scope"}},
	}
	result := knowledge.Result{
		ReadAuthorities: refs,
		Proposal: &knowledge.Proposal{
			ID:              7,
			State:           "pending_filter",
			Text:            "synthetic visible body",
			ReadAuthorities: refs,
		},
	}
	deferred := knowledgeScriptResult(
		interaction.KnowledgeOutcome{
			Result: result,
			Assessment: interaction.KnowledgeAssessmentState{
				Status: interaction.KnowledgeAssessmentDeferred,
				Reason: interaction.KnowledgeDeferredUnavailable,
			},
		},
	)
	record := agenthost.ScriptToolRecord{
		Memory:  &knowledge.Command{Name: knowledge.Suggest},
		Outcome: agent.ScriptToolResult{Name: "knowledge.suggest"},
	}
	visible, err := (&Bot{}).scriptHost().EncodeResult(&record, deferred, "", maxScriptReadBytes)
	require.NoError(t, err)
	assert.NotContains(t, string(visible), "read_authorities")
	assert.NotContains(t, string(visible), "host-only-review-scope")
	assert.Contains(t, string(visible), "synthetic visible body")
	assert.Contains(t, string(visible), `"assessment":{"status":"deferred","reason":"unavailable"}`)
	assert.Equal(t, refs, record.ResultAuthorities)
	assert.Equal(t, refs, result.Proposal.ReadAuthorities)
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	var restored agenthost.ScriptToolRecord
	require.NoError(t, json.Unmarshal(raw, &restored))
	authorities, err := agenthost.ScriptCallReadAuthorities("actor", restored)
	require.NoError(t, err)
	assert.Equal(t, refs, authorities)
}
