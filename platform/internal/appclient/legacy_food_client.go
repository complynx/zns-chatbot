package appclient

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func (c Client) FoodViewForReview(
	ctx context.Context,
	owner, event, order string,
	review bool,
) (legacyfood.View, error) {
	if review {
		return c.FoodReview(ctx, owner, event, order)
	}
	return c.FoodView(ctx, owner, event, order)
}
