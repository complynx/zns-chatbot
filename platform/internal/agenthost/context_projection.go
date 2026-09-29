package agenthost

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func (b ContextBuilder) addFoodHint(ctx context.Context, owner string, input *agent.Input) error {
	input.Food = nil
	command, found, err := b.PendingFood.Read(ctx, owner)
	if err != nil || !found {
		return err
	}
	view, err := b.Reads.FoodView(ctx, owner, command.EventID, command.OrderID)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	if err != nil {
		return err
	}
	payment := view.Order.MealPayment
	if command.Kind == legacyfood.Activity {
		payment = view.Order.ActivityPayment
	}
	if view.Order.Version != command.Version || payment.Generation != command.Generation || payment.Locked() {
		return nil
	}
	input.Food = &agent.FoodHint{
		EventID: view.Event.ID,
		OrderID: view.Order.ID,
		Kind:    payment.Kind,
		State:   payment.Status,
	}
	return nil
}

func addAssets(ctx context.Context, source AssetSource, input *agent.Input) error {
	if source == nil {
		return nil
	}
	assets, err := source.Describe(ctx)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		assets = agent.AssetContext{Items: []agent.AssetObservation{{Kind: "custom_emoji", Status: "unavailable"}}}
	}
	if len(assets.Items) > 0 || assets.Omitted > 0 {
		input.Assets = &assets
	}
	return nil
}
