package agenthost_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

type knowledgeReadFixture struct {
	agenthost.KnowledgeDomain
	agenthost.KnowledgeReadStore

	calls []string
}

func (f *knowledgeReadFixture) ReserveKnowledge(context.Context, string, int64, agent.KnowledgeProposal) (int, error) {
	f.calls = append(f.calls, "reserve")
	return 0, nil
}

func (f *knowledgeReadFixture) MemoryDeletions(context.Context, string) (knowledge.MemoryDeletionState, error) {
	f.calls = append(f.calls, "generation")
	return knowledge.MemoryDeletionState{}, nil
}

func (f *knowledgeReadFixture) KnowledgePage(context.Context, string, knowledge.Query) (knowledge.FactPage, error) {
	f.calls = append(f.calls, "page")
	return knowledge.FactPage{}, context.Canceled
}

func TestKnowledgeReadCancellationKeepsConsumedReservation(t *testing.T) {
	t.Parallel()
	f := &knowledgeReadFixture{}
	reader := agenthost.KnowledgeReader{Domain: f, Store: f}
	input := agent.Input{Knowledge: &agent.KnowledgeContext{Remaining: 1}}
	err := reader.ReadKnowledge(t.Context(), "owner", 73, agent.KnowledgeProposal{Name: agent.KnowledgeRead}, &input)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, []string{"reserve", "generation", "page"}, f.calls)
}

func TestKnowledgeReadExhaustionPreventsDomainIO(t *testing.T) {
	t.Parallel()
	f := &knowledgeReadFixture{}
	reader := agenthost.KnowledgeReader{Domain: f, Store: f}
	input := agent.Input{Knowledge: &agent.KnowledgeContext{}}
	err := reader.ReadKnowledge(t.Context(), "owner", 74, agent.KnowledgeProposal{Name: agent.KnowledgeRead}, &input)
	require.EqualError(t, err, "knowledge read budget exhausted")
	require.Empty(t, f.calls)
}
