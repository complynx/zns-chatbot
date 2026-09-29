package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c Client) ResolveLegacyOrderCallback(
	ctx context.Context,
	owner string,
	request orders.LegacyCallbackRequest,
) (orders.LegacyCallbackBinding, error) {
	var binding orders.LegacyCallbackBinding
	err := c.Call(ctx, owner, http.MethodPost, "/v1/legacy-order-callbacks/resolve", request, &binding)
	return binding, err
}
