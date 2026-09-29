package bot

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

const knowledgeTopicQuery = "topic"
const knowledgeEventQuery = "event"
const knowledgeKeyQuery = "key"

func (c APIClient) KnowledgePage(ctx context.Context, owner string, query knowledge.Query) (knowledge.FactPage, error) {
	values := url.Values{
		knowledgeEventQuery: {query.Event},
		knowledgeTopicQuery: {query.Topic},
		"q":                 {query.Text},
		"cursor":            {query.Cursor},
	}
	var result knowledge.FactPage
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/page?"+values.Encode(), nil, &result)
	return result, err
}

func (c APIClient) KnowledgeScopes(ctx context.Context, owner string) ([]knowledge.Scope, error) {
	var result []knowledge.Scope
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/scopes", nil, &result)
	return result, err
}

func (c APIClient) KnowledgeFact(ctx context.Context, owner, event, topic, key string) (knowledge.Fact, error) {
	values := url.Values{knowledgeEventQuery: {event}, knowledgeTopicQuery: {topic}, knowledgeKeyQuery: {key}}
	var result knowledge.Fact
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/fact?"+values.Encode(), nil, &result)
	return result, err
}

func (c APIClient) Knowledge(ctx context.Context, owner string, query knowledge.Query) ([]knowledge.Fact, error) {
	values := url.Values{knowledgeEventQuery: {query.Event}, knowledgeTopicQuery: {query.Topic}, "q": {query.Text}}
	var result []knowledge.Fact
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge?"+values.Encode(), nil, &result)
	return result, err
}

func (c APIClient) KnowledgeProposals(
	ctx context.Context,
	owner string,
	query knowledge.ProposalQuery,
) ([]knowledge.Proposal, error) {
	values := url.Values{
		knowledgeEventQuery: {query.Event},
		"review":            {strconv.FormatBool(query.ReviewQueue)},
		"after":             {strconv.FormatInt(query.After, 10)},
	}
	var result []knowledge.Proposal
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/proposals?"+values.Encode(), nil, &result)
	return result, err
}

func (c APIClient) Memos(ctx context.Context, owner string) ([]knowledge.Memo, error) {
	var result []knowledge.Memo
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/memos", nil, &result)
	return result, err
}

func (c APIClient) Memo(ctx context.Context, owner, key string) (knowledge.Memo, error) {
	var result knowledge.Memo
	err := c.call(ctx, owner, http.MethodGet, "/v1/me/memos/"+url.PathEscape(key), nil, &result)
	return result, err
}

func (c APIClient) ExecuteKnowledge(
	ctx context.Context,
	owner string,
	command knowledge.Command,
) (knowledge.Result, error) {
	var result knowledge.Result
	err := c.call(ctx, owner, http.MethodPost, "/v1/knowledge/actions", command, &result)
	return result, err
}
