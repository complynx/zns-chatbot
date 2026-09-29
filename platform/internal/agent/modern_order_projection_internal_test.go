package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestModernOrderProjectionReachesBothPlanningCalls(t *testing.T) {
	t.Parallel()
	for _, letter := range []string{"a", "Ж"} {
		t.Run(letter, func(t *testing.T) {
			t.Parallel()
			key := strings.Repeat(letter, 262144/len(letter))
			choice := orders.Choice{Extras: map[string]orders.Money{key: 100}, Total: 100}
			input := Input{
				Text:         "Show my full order",
				Orders:       []OrderSummary{{ID: "selected", Version: 7, State: "unpaid", Choice: choice}},
				Extras:       map[string]orders.Extra{key: {Price: 100}},
				OrderHistory: []orders.Change{{OrderID: "selected", Extras: choice.Extras}},
			}
			before, err := json.Marshal(input)
			require.NoError(t, err)
			calls := 0
			_, err = planWithSkills(t.Context(), input, func(_ context.Context, prompt providerPrompt) (string, error) {
				calls++
				assert.Less(t, len(prompt.input), maxInputBytes)
				var projected Input
				require.NoError(t, json.NewDecoder(strings.NewReader(string(prompt.input))).Decode(&projected))
				require.Len(t, projected.Orders, 1)
				assert.Equal(t, "selected", projected.Orders[0].ID)
				assert.EqualValues(t, 7, projected.Orders[0].Version)
				assert.Equal(t, "unpaid", projected.Orders[0].State)
				assert.Equal(t, choice.Total, projected.Orders[0].Choice.Total)
				assert.True(t, projected.Orders[0].DetailsIncomplete)
				assert.Contains(t, projected.OrderContextNotice, "orders.inspect")
				assert.Contains(t, projected.OrderContextNotice, "orders.event")
				assert.Contains(t, projected.OrderContextNotice, "orders.history.read")
				if calls == 1 {
					return `{"skills":["orders"],"reply_language":"en"}`, nil
				}
				return emptyActionsPlan, nil
			})
			require.NoError(t, err)
			assert.Equal(t, 2, calls)
			after, err := json.Marshal(input)
			require.NoError(t, err)
			assert.Equal(t, before, after, "provider projection must not change authoritative host state")
		})
	}
}

func TestModernOrderProjectionPreservesSmallTypedInput(t *testing.T) {
	t.Parallel()
	input := Input{
		Orders: []OrderSummary{
			{ID: "small", Choice: orders.Choice{Extras: map[string]orders.Money{"shuttle": 100}}},
		},
		Extras:       orders.Extras(),
		OrderHistory: []orders.Change{{OrderID: "small", Extras: map[string]orders.Money{"shuttle": 100}}},
	}
	assert.Equal(t, input, projectModernOrders(input))
}
