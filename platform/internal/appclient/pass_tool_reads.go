package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassToolCapabilities(ctx context.Context, owner string) (passbooking.ToolCapabilities, error) {
	if c.LocalRegistration != nil {
		return directRegistration(
			ctx,
			c,
			owner,
			func(service passbooking.Service, actor string) (passbooking.ToolCapabilities, error) {
				return service.ToolCapabilities(ctx, actor)
			},
		)
	}
	var result passbooking.ToolCapabilities
	err := c.Call(ctx, owner, http.MethodGet, "/v1/passes/tool-capabilities", nil, &result)
	if err != nil {
		return passbooking.ToolCapabilities{}, err
	}
	return result, nil
}

func (c Client) PassTierStatus(ctx context.Context, owner, event string) (passbooking.TierStatus, error) {
	if c.LocalRegistration != nil {
		return directRegistration(
			ctx,
			c,
			owner,
			func(service passbooking.Service, actor string) (passbooking.TierStatus, error) {
				return service.TierStatus(ctx, actor, event)
			},
		)
	}
	var result passbooking.TierStatus
	err := c.Call(ctx, owner, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/tiers", nil, &result)
	if err != nil {
		return passbooking.TierStatus{}, err
	}
	return result, nil
}
