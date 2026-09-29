package appclient

import (
	"context"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func (c Client) FoodCapabilities(ctx context.Context, owner string) (legacyfood.OwnerCapabilities, error) {
	var result legacyfood.OwnerCapabilities
	err := c.Call(ctx, owner, http.MethodGet, "/v1/food/capabilities", nil, &result)
	return result, err
}
