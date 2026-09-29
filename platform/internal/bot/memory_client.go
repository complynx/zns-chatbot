package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const memoryCursorQuery = "cursor"

func memoryQueryValues(q knowledge.MemoryQuery) url.Values {
	return url.Values{
		"namespace":         {q.Namespace},
		knowledgeEventQuery: {q.Event},
		"topic":             {q.Topic},
		"q":                 {q.Text},
		"mode":              {q.Mode},
		"cursor":            {q.Cursor},
	}
}

func (c APIClient) MemorySummary(
	ctx context.Context,
	owner string,
	query knowledge.MemoryQuery,
) (knowledge.MemoryOverview, error) {
	var result knowledge.MemoryOverview
	err := c.call(ctx, owner, http.MethodGet, "/v1/memory/summary?"+memoryQueryValues(query).Encode(), nil, &result)
	return result, err
}

func (c APIClient) memoryRead(ctx context.Context, owner, operation string, query url.Values, result any) error {
	return c.call(ctx, owner, http.MethodGet, "/v1/memory/"+operation+"?"+query.Encode(), nil, result)
}
