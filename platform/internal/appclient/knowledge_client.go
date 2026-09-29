package appclient

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func (c Client) KnowledgePage(ctx context.Context, owner string, query knowledge.Query) (knowledge.FactPage, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.FactPage, error) {
			return s.RetrievePage(ctx, actor, query)
		})
	}

	values := url.Values{
		knowledgeEventQuery: {query.Event},
		knowledgeTopicQuery: {query.Topic},
		"q":                 {query.Text},
		memoryCursorQuery:   {query.Cursor},
	}
	var result knowledge.FactPage
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/knowledge/page?"+values.Encode(), nil, &result)
	for i := range result.Facts {
		result.Facts[i].ReadAuthorities = readsource.CloneAuthorities(result.ReadAuthorities)
	}
	return result, err
}

func (c Client) KnowledgeScopes(ctx context.Context, owner string) ([]knowledge.Scope, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) ([]knowledge.Scope, error) {
			return s.Scopes(ctx, actor)
		})
	}

	var result []knowledge.Scope
	err := c.Call(ctx, owner, http.MethodGet, "/v1/knowledge/scopes", nil, &result)
	return result, err
}

func (c Client) KnowledgeFact(ctx context.Context, owner, event, topic, key string) (knowledge.Fact, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Fact, error) {
			return s.Fact(ctx, actor, event, topic, key)
		})
	}

	values := url.Values{knowledgeEventQuery: {event}, knowledgeTopicQuery: {topic}, knowledgeKeyQuery: {key}}
	var result knowledge.Fact
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/knowledge/fact?"+values.Encode(), nil, &result)
	return result, err
}

func (c Client) Knowledge(ctx context.Context, owner string, query knowledge.Query) ([]knowledge.Fact, error) {
	if c.LocalKnowledge != nil {
		query.Cursor = ""
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) ([]knowledge.Fact, error) {
			return s.Retrieve(ctx, actor, query)
		})
	}

	values := url.Values{knowledgeEventQuery: {query.Event}, knowledgeTopicQuery: {query.Topic}, "q": {query.Text}}
	var result knowledge.FactPage
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/knowledge?"+values.Encode(), nil, &result)
	for i := range result.Facts {
		result.Facts[i].ReadAuthorities = readsource.CloneAuthorities(result.ReadAuthorities)
	}
	return result.Facts, err
}

func (c Client) KnowledgeProposals(
	ctx context.Context,
	owner string,
	query knowledge.ProposalQuery,
) ([]knowledge.Proposal, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) ([]knowledge.Proposal, error) {
			return s.Proposals(ctx, actor, query)
		})
	}

	values := url.Values{
		knowledgeEventQuery: {query.Event},
		"review":            {strconv.FormatBool(query.ReviewQueue)},
		"after":             {strconv.FormatInt(query.After, 10)},
	}
	var result knowledge.ProposalPage
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/knowledge/proposals?"+values.Encode(), nil, &result)
	for i := range result.Items {
		result.Items[i].ReadAuthorities = readsource.CloneAuthorities(result.ReadAuthorities)
	}
	return result.Items, err
}

func (c Client) Memos(ctx context.Context, owner string) ([]knowledge.Memo, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) ([]knowledge.Memo, error) {
			return s.Memos(ctx, actor)
		})
	}

	var result knowledge.MemoPage
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/me/memos", nil, &result)
	for i := range result.Items {
		result.Items[i].ReadAuthorities = readsource.CloneAuthorities(result.ReadAuthorities)
	}
	return result.Items, err
}

func (c Client) Memo(ctx context.Context, owner, key string) (knowledge.Memo, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Memo, error) {
			return s.Memo(ctx, actor, key)
		})
	}

	var result knowledge.Memo
	err := c.knowledgeCall(ctx, owner, http.MethodGet, "/v1/me/memos/"+url.PathEscape(key), nil, &result)
	return result, err
}

func (c Client) ExecuteKnowledge(
	ctx context.Context,
	owner string,
	command knowledge.Command,
) (knowledge.Result, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Result, error) {
			return s.Execute(ctx, actor, command)
		})
	}

	var result knowledge.Result
	err := c.knowledgeCall(ctx, owner, http.MethodPost, "/v1/knowledge/actions", command, &result)
	result.RestoreReadAuthorities()
	return result, err
}
