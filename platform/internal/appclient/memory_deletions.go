package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (c Client) MemoryDeletions(ctx context.Context, owner string) (knowledge.MemoryDeletionState, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(
			ctx,
			c,
			owner,
			func(s knowledge.Service, actor string) (knowledge.MemoryDeletionState, error) {
				return s.MemoryDeletions(ctx, actor)
			},
		)
	}
	var result knowledge.MemoryDeletionState
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/memory/deletions", nil, &result)
	return result, err
}
