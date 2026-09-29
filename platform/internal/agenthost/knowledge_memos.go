package agenthost

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

// Memos binds list reads to the same deletion generation as exact reads. Keeping a
// second unversioned copy in KnowledgeContext.Memos would bypass sanitization.
func (k KnowledgeReader) Memos(
	ctx context.Context,
	owner string,
	input *agent.KnowledgeContext,
) ([]knowledge.Memo, error) {
	state, err := k.Domain.MemoryDeletions(ctx, owner)
	if err != nil {
		return nil, err
	}
	memos, err := k.Domain.Memos(ctx, owner)
	if err != nil {
		return nil, err
	}
	reads := make([]agent.KnowledgeReadResult, 0, len(memos))
	for index := range memos {
		reads = append(reads, agent.KnowledgeReadResult{
			Request:     agent.KnowledgeProposal{Name: agent.KnowledgeMemoRead, FactKey: memos[index].Key},
			MemoryState: state, Memo: &memos[index],
		})
	}
	input.Reads = append(reads, input.Reads...)
	return memos, nil
}
