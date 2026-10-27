package orders_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func mealCatalog() orders.MealCatalog {
	start := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	items := []orders.MealItem{
		{ID: "soup", Name: orders.Label{EN: "Soup", RU: "Суп"}, Category: orders.MealSoup, PriceMinor: 125},
		{ID: "main", Name: orders.Label{EN: "Main", RU: "Горячее"}, Category: orders.MealMain, PriceMinor: 250},
		{ID: "side", Name: orders.Label{EN: "Side", RU: "Гарнир"}, Category: orders.MealSide, PriceMinor: 150},
		{ID: "salad", Name: orders.Label{EN: "Salad", RU: "Салат"}, Category: orders.MealSalad, PriceMinor: 175},
	}
	service := orders.MealService{
		StartsAt:      start,
		EndsAt:        start.Add(time.Hour),
		SalesOpensAt:  start.Add(-48 * time.Hour),
		SalesClosesAt: start.Add(-time.Hour),
		Items:         items,
	}
	dinner := service
	dinner.StartsAt = start.Add(12 * time.Hour)
	dinner.EndsAt = dinner.StartsAt.Add(time.Hour)
	dinner.Items = []orders.MealItem{{ID: "tea", Name: orders.Label{EN: "Tea", RU: "Чай"}, PriceMinor: 50}}
	return orders.MealCatalog{Currency: "RUB", CurrencyScale: 0, Days: []orders.MealDay{
		{
			ID:   "day_one",
			Name: orders.Label{EN: "Day one", RU: "Первый день"},
			Lunch: &orders.MealLunch{
				Service: service,
				Combos:  &orders.MealComboPrices{WithoutSoupMinor: 555, WithSoupMinor: 665},
			},
			Dinner: &dinner,
		},
	}}
}

func TestMealDraftKeepsIncompleteComboWithoutPayableQuote(t *testing.T) {
	t.Parallel()
	d := mealCatalog()
	day2 := d.Days[0]
	day2.ID = "day_two"
	d.Days = append(d.Days, day2)
	result, err := orders.EvaluateMeals(d, []orders.MealDayChoice{
		{
			DayID: "day_one",
			Lunch: &orders.LunchChoice{
				Mode:    orders.LunchComboWithSoup,
				SoupID:  "soup",
				MainID:  "main",
				SideID:  "side",
				SaladID: "salad",
			},
			DinnerIDs: []string{"tea"},
		},
		{DayID: "day_two", Lunch: &orders.LunchChoice{Mode: orders.LunchComboWithoutSoup, MainID: "main"}},
	})
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Nil(t, result.Quote)
	require.Equal(t, int64(715), result.KnownAmountMinor)
	require.Equal(
		t,
		[]orders.MealMissingChoice{{DayID: "day_two", Field: "side"}, {DayID: "day_two", Field: "salad"}},
		result.Missing,
	)
	require.Equal(t, "main", result.Choices[1].Lunch.MainID)
	result, err = orders.EvaluateMeals(
		d,
		[]orders.MealDayChoice{
			result.Choices[0],
			{
				DayID: "day_two",
				Lunch: &orders.LunchChoice{
					Mode:    orders.LunchComboWithoutSoup,
					MainID:  "main",
					SideID:  "side",
					SaladID: "salad",
				},
			},
		},
	)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.NotNil(t, result.Quote)
	require.Equal(t, int64(1270), result.Quote.TotalMinor)
	require.Equal(t, "RUB", result.Quote.Currency)
	require.Equal(t, 0, result.Quote.CurrencyScale)
}

func TestMealModesUseFixedComboAndIndividualPrices(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		lunch  *orders.LunchChoice
		amount int64
	}{
		"no lunch":         {&orders.LunchChoice{Mode: orders.LunchNone}, 0},
		"empty individual": {&orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{}}, 0},
		"individual":       {&orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"main", "soup"}}, 375},
		"without soup": {
			&orders.LunchChoice{Mode: orders.LunchComboWithoutSoup, MainID: "main", SideID: "side", SaladID: "salad"},
			555,
		},
		"with soup": {
			&orders.LunchChoice{
				Mode:    orders.LunchComboWithSoup,
				SoupID:  "soup",
				MainID:  "main",
				SideID:  "side",
				SaladID: "salad",
			},
			665,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := orders.EvaluateMeals(
				mealCatalog(),
				[]orders.MealDayChoice{{DayID: "day_one", Lunch: tc.lunch}},
			)
			require.NoError(t, err)
			require.True(t, result.Complete)
			require.Equal(t, tc.amount, result.Quote.TotalMinor)
		})
	}
	result, err := orders.EvaluateMeals(mealCatalog(), nil)
	require.NoError(t, err)
	require.False(t, result.Complete)
	require.Nil(t, result.Quote)
	require.Equal(t, []orders.MealMissingChoice{{DayID: "day_one", Field: "lunch"}}, result.Missing)
}

func TestMealDraftRejectsMalformedComposition(t *testing.T) {
	t.Parallel()
	for name, choice := range map[string]orders.MealDayChoice{
		"foreign day": {DayID: "elsewhere"},
		"foreign item": {
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"missing"}},
		},
		"dinner item in lunch": {
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"tea"}},
		},
		"lunch item in dinner": {DayID: "day_one", DinnerIDs: []string{"soup"}},
		"duplicate item":       {DayID: "day_one", DinnerIDs: []string{"tea", "tea"}},
		"wrong category": {
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchComboWithSoup, SoupID: "main"},
		},
		"no-lunch with components": {DayID: "day_one", Lunch: &orders.LunchChoice{Mode: orders.LunchNone, MainID: "main"}},
		"individual with combo": {
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual, SaladID: "salad"},
		},
		"combo with individual": {
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchComboWithSoup, IndividualIDs: []string{"main"}},
		},
		"without soup includes soup": {
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchComboWithoutSoup, SoupID: "soup"},
		},
		"unknown mode": {DayID: "day_one", Lunch: &orders.LunchChoice{Mode: "unknown"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := orders.EvaluateMeals(mealCatalog(), []orders.MealDayChoice{choice})
			require.ErrorIs(t, err, orders.ErrMealChoices)
		})
	}
	choice := orders.MealDayChoice{DayID: "day_one"}
	_, err := orders.EvaluateMeals(mealCatalog(), []orders.MealDayChoice{choice, choice})
	require.ErrorIs(t, err, orders.ErrMealChoices)
	d := mealCatalog()
	d.Days[0].Lunch.Combos = nil
	_, err = orders.EvaluateMeals(
		d,
		[]orders.MealDayChoice{{DayID: "day_one", Lunch: &orders.LunchChoice{Mode: orders.LunchComboWithoutSoup}}},
	)
	require.ErrorIs(t, err, orders.ErrMealChoices)
}

func TestMealChoicesHaveStableSetOrderAndNoInputMutation(t *testing.T) {
	t.Parallel()
	a := []orders.MealDayChoice{
		{
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"soup", "main"}},
		},
	}
	b := []orders.MealDayChoice{
		{
			DayID: "day_one",
			Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"main", "soup"}},
		},
	}
	one, err := orders.EvaluateMeals(mealCatalog(), a)
	require.NoError(t, err)
	two, err := orders.EvaluateMeals(mealCatalog(), b)
	require.NoError(t, err)
	require.Equal(t, one, two)
	require.Equal(t, []string{"soup", "main"}, a[0].Lunch.IndividualIDs)
}

func TestMealCatalogValidatesExplicitServicesAndMoney(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*orders.MealCatalog){
		"duplicate day": func(d *orders.MealCatalog) { d.Days = append(d.Days, d.Days[0]) },
		"no service":    func(d *orders.MealCatalog) { d.Days[0].Lunch = nil; d.Days[0].Dinner = nil },
		"duplicate item": func(d *orders.MealCatalog) {
			s := &d.Days[0].Lunch.Service
			s.Items = append(
				s.Items,
				s.Items[0],
			)
		},
		"invalid category": func(d *orders.MealCatalog) { d.Days[0].Lunch.Service.Items[0].Category = "magic" },
		"combo category absent": func(d *orders.MealCatalog) {
			d.Days[0].Lunch.Service.Items = d.Days[0].Lunch.Service.Items[1:]
		},
		"negative combo": func(d *orders.MealCatalog) { d.Days[0].Lunch.Combos.WithSoupMinor = -1 },
		"unsafe price":   func(d *orders.MealCatalog) { d.Days[0].Dinner.Items[0].PriceMinor = orders.MaxPrice + 1 },
		"no translation": func(d *orders.MealCatalog) { d.Days[0].Name.RU = "" },
		"no date":        func(d *orders.MealCatalog) { d.Days[0].Lunch.Service.StartsAt = time.Time{} },
		"bad end": func(d *orders.MealCatalog) {
			d.Days[0].Lunch.Service.EndsAt = d.Days[0].Lunch.Service.StartsAt
		},
		"bad sales": func(d *orders.MealCatalog) {
			d.Days[0].Lunch.Service.SalesClosesAt = d.Days[0].Lunch.Service.SalesOpensAt
		},
		"submicrosecond": func(d *orders.MealCatalog) {
			d.Days[0].Lunch.Service.StartsAt = d.Days[0].Lunch.Service.StartsAt.Add(time.Nanosecond)
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := mealCatalog()
			change(&d)
			_, err := orders.NormalizeMeals(d)
			require.ErrorIs(t, err, orders.ErrMealCatalog)
		})
	}
	d, err := orders.NormalizeMeals(mealCatalog())
	require.NoError(t, err)
	s := d.Days[0].Lunch.Service
	require.False(t, s.SalesOpen(s.SalesOpensAt.Add(-time.Microsecond)))
	require.True(t, s.SalesOpen(s.SalesOpensAt))
	require.True(t, s.SalesOpen(s.SalesClosesAt.Add(-time.Microsecond)))
	require.False(t, s.SalesOpen(s.SalesClosesAt))
	zone, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	require.Equal(t, "2026-10-25T02:30:00+02:00", s.StartsAt.In(zone).Format(time.RFC3339))
	require.Equal(t, "2026-10-25T02:30:00+01:00", s.EndsAt.In(zone).Format(time.RFC3339))
}

func TestMealQuoteRejectsAmountOverflowAndAllowsZero(t *testing.T) {
	t.Parallel()
	d := mealCatalog()
	d.Days[0].Lunch.Service.Items[0].PriceMinor = orders.MaxPrice
	d.Days[0].Lunch.Service.Items[1].PriceMinor = 1
	_, err := orders.EvaluateMeals(
		d,
		[]orders.MealDayChoice{
			{
				DayID: "day_one",
				Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual, IndividualIDs: []string{"soup", "main"}},
			},
		},
	)
	require.ErrorIs(t, err, orders.ErrAmount)
	result, err := orders.EvaluateMeals(
		d,
		[]orders.MealDayChoice{{DayID: "day_one", Lunch: &orders.LunchChoice{Mode: orders.LunchNone}}},
	)
	require.NoError(t, err)
	require.Zero(t, result.Quote.TotalMinor)
}

func TestDinnerOnlyMealDraftNeedsNoLunchChoice(t *testing.T) {
	t.Parallel()
	d := mealCatalog()
	d.Days[0].Lunch = nil
	result, err := orders.EvaluateMeals(d, nil)
	require.NoError(t, err)
	require.True(t, result.Complete)
	require.Zero(t, result.Quote.TotalMinor)
	result, err = orders.EvaluateMeals(d, []orders.MealDayChoice{{DayID: "day_one", DinnerIDs: []string{"tea"}}})
	require.NoError(t, err)
	require.Equal(t, int64(50), result.Quote.TotalMinor)
	_, err = orders.EvaluateMeals(
		d,
		[]orders.MealDayChoice{{DayID: "day_one", Lunch: &orders.LunchChoice{Mode: orders.LunchNone}}},
	)
	require.ErrorIs(t, err, orders.ErrMealChoices)
}

func TestMealCatalogRetainsExplicitPaymentDeadlineWithoutInferringOne(t *testing.T) {
	t.Parallel()
	catalog := mealCatalog()
	legacy, err := orders.NormalizeMeals(catalog)
	require.NoError(t, err)
	require.Nil(t, legacy.PaymentDeadline)
	deadline := time.Date(2026, 10, 23, 18, 0, 0, 0, time.FixedZone("event", 3*60*60))
	catalog.PaymentDeadline = &deadline
	normalized, err := orders.NormalizeMeals(catalog)
	require.NoError(t, err)
	require.Equal(t, "2026-10-23T15:00:00Z", normalized.PaymentDeadline.Format(time.RFC3339))
	deadline = deadline.Add(time.Hour)
	require.Equal(t, "2026-10-23T15:00:00Z", normalized.PaymentDeadline.Format(time.RFC3339))
	for _, invalid := range []time.Time{{}, time.Date(2026, 10, 23, 18, 0, 0, 1, time.UTC)} {
		catalog.PaymentDeadline = &invalid
		_, err = orders.NormalizeMeals(catalog)
		require.ErrorIs(t, err, orders.ErrMealCatalog)
	}
}
