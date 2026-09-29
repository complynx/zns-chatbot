package appclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
)

func (c Client) PassCapabilities(ctx context.Context, actor, event string) (passbooking.Capabilities, error) {
	if c.LocalRegistration != nil {
		return c.localPassCapabilities(ctx, actor, event)
	}
	var value passbooking.Capabilities
	err := c.Call(ctx, actor, http.MethodGet, "/v1/passes/events/"+url.PathEscape(event)+"/capabilities", nil, &value)
	return registrationHTTPResult(value, err)
}
