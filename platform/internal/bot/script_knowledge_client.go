package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
)

func (c APIClient) knowledgeCapabilities(ctx context.Context, owner string) (knowledge.Capabilities, error) {
	var result knowledge.Capabilities
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/capabilities", nil, &result)
	return result, err
}
func (c APIClient) knowledgeScope(ctx context.Context, owner, event string) (knowledge.Scope, error) {
	var result knowledge.Scope
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/scope?event="+url.QueryEscape(event), nil, &result)
	return result, err
}
func (c APIClient) knowledgeScopePage(ctx context.Context, owner, cursor string) (knowledge.ScopePage, error) {
	var result knowledge.ScopePage
	err := c.call(ctx, owner, http.MethodGet, "/v1/knowledge/scopes/page?cursor="+url.QueryEscape(cursor), nil, &result)
	return result, err
}
