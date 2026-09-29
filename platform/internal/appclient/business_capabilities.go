package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func (c Client) BusinessCapabilities(ctx context.Context, owner, event string) (agent.BusinessCapabilities, error) {
	var value agent.BusinessCapabilities
	err := c.Call(ctx, owner, http.MethodGet, "/v1/business-capabilities?event="+url.QueryEscape(event), nil, &value)
	return value, err
}
