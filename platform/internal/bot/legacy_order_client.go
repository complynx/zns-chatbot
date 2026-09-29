package bot

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func (c APIClient) ResolveLegacyOrderCallback(
	ctx context.Context,
	owner string,
	request orders.LegacyCallbackRequest,
) (orders.LegacyCallbackBinding, error) {
	var binding orders.LegacyCallbackBinding
	err := c.call(ctx, owner, http.MethodPost, "/v1/legacy-order-callbacks/resolve", request, &binding)
	return binding, err
}
