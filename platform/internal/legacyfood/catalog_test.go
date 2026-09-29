package legacyfood_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func TestMealMenuVariantsAndHistoricalIndices(t *testing.T) {
	t.Parallel()
	menu := legacyfood.Menu{
		"friday": {
			Lunch:  []legacyfood.Dish{{Price: 18500}, {Price: 38000}, {Price: 10000}, {Price: 15000}},
			Dinner: []legacyfood.Dish{{Price: 25000}},
		},
	}
	prices := legacyfood.MealPrices{WithSoup: 66500, WithoutSoup: 55500}
	for _, test := range []struct {
		name, raw string
		total     legacyfood.Amount
		complete  bool
	}{
		{"missing", `{}`, 0, false},
		{"no lunch", `{"friday":{"lunch":{"type":"no-lunch"}}}`, 0, true},
		{"empty individual", `{"friday":{"lunch":{"type":"individual-items","items":[]}}}`, 0, true},
		{"individual and dinner", `{"friday":{"lunch":{"type":"individual-items","items":[0,"1"]},"dinner":["0"]}}`, 81500, true},
		{"with soup", `{"friday":{"lunch":{"type":"combo-with-soup","items":{"soup_index":0,"main_index":1,"side_index":2,"salad_index":3}}}}`, 66500, true},
		{"without soup", `{"friday":{"lunch":{"type":"combo-no-soup","items":{"main_index":1,"side_index":2,"salad_index":3}}}}`, 55500, true},
		{"incomplete combo", `{"friday":{"lunch":{"type":"combo-with-soup","items":{"main_index":1,"side_index":2,"salad_index":3}},"dinner":[0]}}`, 25000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var selection legacyfood.MealSelection
			require.NoError(t, json.Unmarshal([]byte(test.raw), &selection))
			quote, err := legacyfood.QuoteMeals(menu, prices, selection)
			require.NoError(t, err)
			assert.Equal(t, test.total, quote.Total)
			assert.Equal(t, test.complete, quote.Complete)
		})
	}
	var selection legacyfood.MealSelection
	require.NoError(
		t,
		json.Unmarshal([]byte(`{"friday":{"lunch":{"type":"individual-items","items":[99]}}}`), &selection),
	)
	_, err := legacyfood.QuoteMeals(menu, prices, selection)
	require.EqualError(t, err, "food_invalid_item_index")
}

func TestActivityPriceMatrixAndGroupReplacement(t *testing.T) {
	t.Parallel()
	prices := legacyfood.ActivityPrices{
		Party:           200000,
		PartyAndClasses: 250000,
		AllClasses:      200000,
		Yoga:            75000,
		Cacao:           100000,
		SoundHealing:    100000,
	}
	names := []string{"open", "yoga", "cacao", "soundhealing"}
	expected := []legacyfood.Amount{
		0,
		200000,
		75000,
		250000,
		100000,
		250000,
		175000,
		250000,
		100000,
		250000,
		175000,
		250000,
		200000,
		250000,
		200000,
		250000,
	}
	for bits, want := range expected {
		selection := legacyfood.Activities{}
		for index, name := range names {
			selection[name] = bits&(1<<index) != 0
		}
		got, err := legacyfood.QuoteActivities(prices, selection)
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
	selected, err := legacyfood.ToggleActivities(legacyfood.Activities{"open": true, "cacao": true}, "classes", false)
	require.NoError(t, err)
	assert.Equal(t, legacyfood.Activities{"yoga": true, "soundhealing": true}, selected)
	selected, err = legacyfood.ToggleActivities(legacyfood.Activities{"cacao": true}, "cacao", false)
	require.NoError(t, err)
	assert.False(t, selected["cacao"])
}
