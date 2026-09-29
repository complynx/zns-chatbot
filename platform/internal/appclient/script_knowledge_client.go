package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (c Client) KnowledgeCapabilities(ctx context.Context, owner string) (knowledge.Capabilities, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Capabilities, error) {
			return s.Capabilities(ctx, actor)
		})
	}

	var result knowledge.Capabilities
	err := c.Call(ctx, owner, http.MethodGet, "/v1/knowledge/capabilities", nil, &result)
	return result, err
}

func (c Client) KnowledgeScope(ctx context.Context, owner, event string) (knowledge.Scope, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.Scope, error) {
			return s.Scope(ctx, actor, event)
		})
	}

	var result knowledge.Scope
	err := c.Call(ctx, owner, http.MethodGet, "/v1/knowledge/scope?event="+url.QueryEscape(event), nil, &result)
	return result, err
}

func (c Client) KnowledgeScopePage(ctx context.Context, owner, cursor string) (knowledge.ScopePage, error) {
	if c.LocalKnowledge != nil {
		return directKnowledge(ctx, c, owner, func(s knowledge.Service, actor string) (knowledge.ScopePage, error) {
			return s.ScopePage(ctx, actor, cursor)
		})
	}

	var result knowledge.ScopePage
	err := c.Call(ctx, owner, http.MethodGet, "/v1/knowledge/scopes/page?cursor="+url.QueryEscape(cursor), nil, &result)
	return result, err
}
