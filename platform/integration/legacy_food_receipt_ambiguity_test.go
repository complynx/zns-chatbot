package integration_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func TestFoodAgentAmbiguousKindsWithOneAvailableTarget(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"Is this for meals or activities?", "Это чек за питание или активности?"} {
		t.Run(text, func(t *testing.T) {
			t.Parallel()
			f, service, order := foodAgentReceiptFixture(t)
			order, err := service.Execute(t.Context(), "alice", legacyfood.Command{
				EventID: order.EventID, OrderID: order.ID, Version: order.Version,
				Name: "save_meals", Key: "zero-meals",
				Meals: legacyfood.MealSelection{"friday": {Lunch: &legacyfood.LunchSelection{Type: "no-lunch"}}},
			})
			require.NoError(t, err)
			order, err = service.Execute(t.Context(), "alice", legacyfood.Command{
				EventID: order.EventID, OrderID: order.ID, Version: order.Version,
				Name: "begin_payment", Kind: legacyfood.Activity, Key: "activity-prep",
			})
			require.NoError(t, err)
			require.NoError(t, f.b.RenderMedia(t.Context(), "alice", 101, foodAgentMediaID))
			f.model.plan = agent.Plan{
				View: agent.MediaView,
				Text: "Receipt selection",
				MediaAction: &agent.MediaProposal{
					MediaID: foodAgentMediaID, Intent: "receipt", OrderID: order.ID, FoodKind: legacyfood.Activity,
				},
			}
			handle(t, f.b, message(8890, 101, text+" "+order.ID))
			current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, legacyfood.Pending, current.ActivityPayment.Status)
			assert.Zero(t, current.ActivityPayment.Generation)
			assert.Equal(t, order.Version, current.Version)
		})
	}
}
