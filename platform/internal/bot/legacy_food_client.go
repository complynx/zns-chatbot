package bot

import (
	"context"
	"net/http"
	"net/url"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func (c APIClient) foodView(ctx context.Context, owner, event, order string, review bool) (legacyfood.View, error) {
	path := "/v1/food/view"
	if review {
		path = "/v1/food/review"
	}
	var view legacyfood.View
	err := c.call(
		ctx,
		owner,
		http.MethodGet,
		path+"?event="+url.QueryEscape(event)+"&order="+url.QueryEscape(order),
		nil,
		&view,
	)
	return view, err
}

func (c APIClient) ExecuteFood(
	ctx context.Context,
	owner string,
	command legacyfood.Command,
) (legacyfood.Order, error) {
	var order legacyfood.Order
	err := c.call(ctx, owner, http.MethodPost, "/v1/food/commands", command, &order)
	return order, err
}

func (c APIClient) FoodView(ctx context.Context, owner, event, order string) (legacyfood.View, error) {
	return c.foodView(ctx, owner, event, order, false)
}

func (c APIClient) FoodReview(ctx context.Context, owner, event, order string) (legacyfood.View, error) {
	return c.foodView(ctx, owner, event, order, true)
}

func (c APIClient) FoodCommand(
	ctx context.Context,
	owner string,
	command legacyfood.Command,
) (legacyfood.Order, error) {
	return c.ExecuteFood(ctx, owner, command)
}

func (c APIClient) FoodQuote(
	ctx context.Context,
	owner, event string,
	meals legacyfood.MealSelection,
) (legacyfood.MealQuote, error) {
	var quote legacyfood.MealQuote
	err := c.call(
		ctx,
		owner,
		http.MethodPost,
		"/v1/food/quote",
		map[string]any{foodEventIDField: event, legacyfood.Meals: meals},
		&quote,
	)
	return quote, err
}

func (c APIClient) FoodLegacyMenu(
	ctx context.Context,
	owner, event, key string,
	meals legacyfood.MealSelection,
) (legacyfood.Order, error) {
	var order legacyfood.Order
	err := c.call(
		ctx,
		owner,
		http.MethodPost,
		"/v1/food/legacy-menu",
		map[string]any{foodEventIDField: event, knowledgeKeyQuery: key, legacyfood.Meals: meals},
		&order,
	)
	return order, err
}

func (c APIClient) ResolveFood(ctx context.Context, owner, event, data string) (legacyfood.Callback, error) {
	var binding legacyfood.Callback
	err := c.call(
		ctx,
		owner,
		http.MethodPost,
		"/v1/food/legacy-callbacks/resolve",
		map[string]string{knowledgeEventQuery: event, "data": data},
		&binding,
	)
	return binding, err
}

func (c APIClient) ExportFood(ctx context.Context, owner, event string) (legacyfood.Export, error) {
	var export legacyfood.Export
	err := c.call(ctx, owner, http.MethodGet, "/v1/food/events/"+url.PathEscape(event)+"/export", nil, &export)
	return export, err
}

const foodEventIDField = "event_id"
