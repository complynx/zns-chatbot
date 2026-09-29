package orders_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestMoneyUsesExactCents(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		input string
		cents orders.Money
	}{
		{"0", 0}, {"0.30", 30}, {"35", 3500}, {"1e-2", 1},
	} {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			var got orders.Money
			require.NoError(t, json.Unmarshal([]byte(tc.input), &got))
			assert.Equal(t, tc.cents, got)
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			var back orders.Money
			require.NoError(t, json.Unmarshal(raw, &back))
			assert.Equal(t, got, back)
		})
	}
	for _, input := range []string{"-1", "0.001", "null", "\"1\"", "true", "1e100", "1e-100"} {
		t.Run("invalid_"+input, func(t *testing.T) {
			t.Parallel()
			var got orders.Money
			assert.Error(t, json.Unmarshal([]byte(input), &got))
		})
	}
}

func TestQuoteRejectsForgedTotalsAndCountsService(t *testing.T) {
	t.Parallel()
	menu, err := orders.Menu()
	require.NoError(t, err)
	var in orders.ChoiceInput
	require.NoError(t, json.Unmarshal([]byte(`{
	"customer":"Алиса", "total":0,
	"days":{"friday":{"total":0,"mealtimes":{"dinner":{"total":0,
	"dishes":[{"name":"caesar","count":2,"price":0,"total":0},{"name":"greek","count":1}],
	"service":{"total":0}}}}},"extras":{"shuttle":0,"total":0}}`), &in))
	got, err := orders.Canonicalize(in, menu, orders.Extras())
	require.NoError(t, err)
	meal := got.Days["friday"].Mealtimes["dinner"]
	assert.Equal(t, orders.Money(3180), meal.Total)
	assert.Equal(t, orders.Money(480), meal.Service.Total)
	assert.ElementsMatch(t, []orders.Line{
		{Name: "fork", Count: 1, Price: 30, Total: 30},
		{Name: "salad_container", Count: 3, Price: 150, Total: 450},
	}, meal.Service.Items)
	assert.Equal(t, orders.Money(9680), got.Total)
	assert.Equal(t, orders.Money(6500), got.Extras["shuttle"])
}

func TestQuoteRejectsInvalidChoices(t *testing.T) {
	t.Parallel()
	menu, err := orders.Menu()
	require.NoError(t, err)
	for _, input := range []string{
		`{"extras":{"excursion_grodno":25}}`,
		`{"extras":{"excursion_grodno_overview":25,"excursion_grodno_gorodnitsa":25}}`,
		`{"extras":{"unknown":1}}`,
		`{"customer":"bad\u0001"}`,
		`{"days":{"monday":{}}}`,
		`{"days":{"friday":{"mealtimes":{"lunch":{}}}}}`,
		`{"days":{"friday":{"mealtimes":{"dinner":{"dishes":[{"name":"caesar","count":0}]}}}}}`,
		`{"days":{"friday":{"mealtimes":{"dinner":{"dishes":[{"name":"white_dew","count":1}]}}}}}`,
	} {
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			var in orders.ChoiceInput
			require.NoError(t, json.Unmarshal([]byte(input), &in))
			_, choiceError := orders.Canonicalize(in, menu, orders.Extras())
			assert.Error(t, choiceError)
		})
	}
}
