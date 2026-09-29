package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

// Refresh at each provider boundary; persisted context cannot preserve a grant.
func (b *Bot) reauthorizeBusinessContext(ctx context.Context, owner string, input *agent.Input) error {
	value, err := b.API.BusinessCapabilities(ctx, owner, b.currentOrderEvent())
	if err != nil {
		return err
	}
	input.Business = &value
	return nil
}

func (c APIClient) BusinessCapabilities(ctx context.Context, owner, event string) (agent.BusinessCapabilities, error) {
	var value agent.BusinessCapabilities
	err := c.call(ctx, owner, http.MethodGet, "/v1/business-capabilities?event="+url.QueryEscape(event), nil, &value)
	return value, err
}
