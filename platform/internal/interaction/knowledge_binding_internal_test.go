package interaction

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func TestKnowledgeCommandsRequireOwnedObservedVersions(t *testing.T) {
	t.Parallel()
	input := &agent.KnowledgeContext{
		Scopes: []knowledge.Scope{{Event: "event", CanCurate: true}},
		Memos:  []knowledge.Memo{{Key: "food", Version: 3}},
	}
	command, err := proposedKnowledgeCommand(
		&agent.KnowledgeProposal{Name: knowledge.MemoSet, FactKey: "food", Text: "vegetarian"},
		input,
	)
	require.NoError(t, err)
	assert.EqualValues(t, 3, command.Version)
	_, err = proposedKnowledgeCommand(
		&agent.KnowledgeProposal{Name: knowledge.MemoSet, FactKey: "missing", Text: "new"},
		input,
	)
	require.Error(t, err, "missing active list entry is not proof of version zero")
	input.Reads = []agent.KnowledgeReadResult{
		{Facts: []knowledge.Fact{{Event: "past", Topic: "travel", Key: "venue", Version: 8, HistoricalFallback: true}}},
	}
	proposal := &agent.KnowledgeProposal{
		Name:    knowledge.Curate,
		Event:   "event",
		Topic:   "travel",
		FactKey: "venue",
		Text:    "new venue",
	}
	_, err = proposedKnowledgeCommand(proposal, input)
	require.Error(t, err)
	input.Reads = []agent.KnowledgeReadResult{
		{Facts: []knowledge.Fact{{Event: "event", Topic: "travel", Key: "venue", Version: 4}}},
	}
	command, err = proposedKnowledgeCommand(proposal, input)
	require.NoError(t, err)
	assert.EqualValues(t, 4, command.Version)
	input.Scopes = nil
	_, err = proposedKnowledgeCommand(proposal, input)
	require.Error(t, err)
}
