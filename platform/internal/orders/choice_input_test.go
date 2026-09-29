package orders_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestChoiceInputCopiesSelectionsWithoutPrices(t *testing.T) {
	t.Parallel()
	original := orders.Choice{
		Customer: "Alice",
		Extras:   map[string]orders.Money{"extra": 700, "total": 700},
		Days: map[string]orders.Day{
			"day": {
				Mealtimes: map[string]orders.Meal{
					"meal": {Dishes: []orders.Line{{Name: "dish", Count: 2, Price: 100, Total: 200}}},
				},
			},
		},
	}
	input := original.Input()
	require.Equal(t, "Alice", input.Customer)
	require.NotContains(t, input.Extras, "total")
	require.JSONEq(t, "0", string(input.Extras["extra"]))
	require.Nil(t, input.Total)
	item := input.Days["day"].Mealtimes["meal"].Dishes[0]
	require.Equal(t, orders.Item{Name: "dish", Count: 2}, item)
	input.Days["day"].Mealtimes["meal"].Dishes[0].Count = 9
	delete(input.Extras, "extra")
	require.EqualValues(t, 2, original.Days["day"].Mealtimes["meal"].Dishes[0].Count)
	require.EqualValues(t, 700, original.Extras["extra"])
}
