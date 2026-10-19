package db_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/db"
	"github.com/complynx/zns-chatbot/internal/orders"
)

func mealTermsFixture(t *testing.T) (db.MealCatalogView, db.MealDraft) {
	t.Helper()
	start := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	service := orders.MealService{
		StartsAt:      start,
		EndsAt:        start.Add(time.Hour),
		SalesOpensAt:  start.Add(-48 * time.Hour),
		SalesClosesAt: start.Add(-time.Hour),
		Items:         []orders.MealItem{},
	}
	for _, category := range []orders.MealCategory{orders.MealSoup, orders.MealMain, orders.MealSide, orders.MealSalad} {
		service.Items = append(
			service.Items,
			orders.MealItem{
				ID:         string(category),
				Name:       orders.Label{EN: "Original " + string(category), RU: "Исходный " + string(category)},
				Category:   category,
				PriceMinor: 100,
			},
		)
	}
	dinner := service
	dinner.StartsAt = start.Add(time.Hour)
	dinner.EndsAt = start.Add(2 * time.Hour)
	dinner.Items = []orders.MealItem{
		{ID: "tea", Name: orders.Label{EN: "Original tea", RU: "Исходный чай"}, PriceMinor: 50},
	}
	catalog := db.MealCatalogView{
		EventID: uuid.MustParse("00000000-0000-4000-8000-000000000001"), Version: 3,
		Document: orders.MealCatalog{Currency: "BYN", CurrencyScale: 2, Days: []orders.MealDay{
			{
				ID:   "friday",
				Name: orders.Label{EN: "Original Friday", RU: "Исходная пятница"},
				Lunch: &orders.MealLunch{
					Service: service,
					Combos:  &orders.MealComboPrices{WithoutSoupMinor: 555, WithSoupMinor: 665},
				},
				Dinner: &dinner,
			},
		}},
		EventVersion: 7, EventName: "Original event", Timezone: "Europe/Berlin",
	}
	choices := []orders.MealDayChoice{
		{
			DayID: "friday",
			Lunch: &orders.LunchChoice{
				Mode:    orders.LunchComboWithSoup,
				SoupID:  "soup",
				MainID:  "main",
				SideID:  "side",
				SaladID: "salad",
			},
			DinnerIDs: []string{"tea"},
		},
	}
	evaluation, err := orders.EvaluateMeals(catalog.Document, choices)
	require.NoError(t, err)
	draft := db.MealDraft{
		EventID:        catalog.EventID,
		EventVersion:   catalog.EventVersion,
		CatalogVersion: catalog.Version,
		Version:        4,
		Timezone:       catalog.Timezone,
		Evaluation:     &evaluation,
	}
	return catalog, draft
}

func TestMealTermsFreezeCompositionAndChargeComboOnce(t *testing.T) {
	t.Parallel()
	catalog, draft := mealTermsFixture(t)
	terms, err := db.FreezeMealDraft(catalog, draft)
	require.NoError(t, err)
	require.Equal(t, int64(715), terms.Quote.TotalMinor)
	require.Len(t, terms.Lines, 2)
	require.Equal(t, int64(665), terms.Lines[0].AmountMinor)
	require.Empty(t, terms.Lines[0].ItemID)
	require.Len(t, terms.Days[0].Lunch.Items, 4)
	require.Equal(t, "Original soup", terms.Days[0].Lunch.Items[0].Name.EN)
	require.Equal(t, orders.MealSoup, terms.Days[0].Lunch.Items[0].Category)
	require.Equal(t, "Original tea", terms.Days[0].Dinner.Items[0].Name.EN)
	require.Equal(t, catalog.Version, terms.CatalogVersion)
	require.Equal(t, draft.Version, terms.DraftVersion)
	require.Equal(t, catalog.EventVersion, terms.EventVersion)
	zone, err := time.LoadLocation(terms.Timezone)
	require.NoError(t, err)
	require.Equal(t, "02:30 +0200", terms.Days[0].Lunch.StartsAt.In(zone).Format("15:04 -0700"))
	require.Equal(t, "02:30 +0100", terms.Days[0].Dinner.StartsAt.In(zone).Format("15:04 -0700"))
	require.Equal(t, catalog.Document.Days[0].Lunch.Service.SalesClosesAt, terms.Days[0].Lunch.SalesClosesAt)
	before, err := json.Marshal(terms)
	require.NoError(t, err)
	again, err := db.FreezeMealDraft(catalog, draft)
	require.NoError(t, err)
	encoded, err := json.Marshal(again)
	require.NoError(t, err)
	require.Equal(t, before, encoded)
	catalog.EventName = "Renamed event"
	catalog.Document.Days[0].Name.EN = "Renamed day"
	catalog.Document.Days[0].Lunch.Service.Items[0].Name.EN = "Renamed soup"
	catalog.Document.Days[0].Lunch.Service.Items = nil
	draft.Evaluation.Choices[0].Lunch.SoupID = "changed"
	draft.Evaluation.Choices[0].DinnerIDs[0] = "changed"
	draft.Evaluation.Quote.TotalMinor = 1
	draft.Evaluation.Lines[0].AmountMinor = 1
	after, err := json.Marshal(terms)
	require.NoError(t, err)
	require.Equal(t, before, after)
}

func TestMealTermsKeepDeclinedEmptyAndDinnerOnlyFacts(t *testing.T) {
	t.Parallel()
	catalog, draft := mealTermsFixture(t)
	day := catalog.Document.Days[0]
	day.ID = "saturday"
	catalog.Document.Days = append(catalog.Document.Days, day)
	day.ID = "sunday"
	day.Lunch = nil
	catalog.Document.Days = append(catalog.Document.Days, day)
	choices := []orders.MealDayChoice{
		{DayID: "friday", Lunch: &orders.LunchChoice{Mode: orders.LunchNone}},
		{DayID: "saturday", Lunch: &orders.LunchChoice{Mode: orders.LunchIndividual}},
		{DayID: "sunday"},
	}
	evaluation, err := orders.EvaluateMeals(catalog.Document, choices)
	require.NoError(t, err)
	draft.Evaluation = &evaluation
	terms, err := db.FreezeMealDraft(catalog, draft)
	require.NoError(t, err)
	require.Zero(t, terms.Quote.TotalMinor)
	require.Empty(t, terms.Lines)
	require.Equal(t, orders.LunchNone, terms.Choices[0].Lunch.Mode)
	require.Equal(t, orders.LunchIndividual, terms.Choices[1].Lunch.Mode)
	require.Nil(t, terms.Choices[2].Lunch)
	require.Nil(t, terms.Days[2].Lunch)
	require.Empty(t, terms.Days[0].Lunch.Items)
	require.Empty(t, terms.Days[1].Lunch.Items)
	require.Empty(t, terms.Days[2].Dinner.Items)
	choices[2].DinnerIDs = []string{"tea"}
	evaluation, err = orders.EvaluateMeals(catalog.Document, choices)
	require.NoError(t, err)
	draft.Evaluation = &evaluation
	terms, err = db.FreezeMealDraft(catalog, draft)
	require.NoError(t, err)
	require.Equal(t, int64(50), terms.Quote.TotalMinor)
	require.Len(t, terms.Lines, 1)
	require.Equal(t, "sunday", terms.Lines[0].DayID)
}

func TestMealTermsRejectMismatchedOrIncompleteSavedDraft(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		change func(*db.MealCatalogView, *db.MealDraft)
	}{
		{"foreign event", func(_ *db.MealCatalogView, d *db.MealDraft) { d.EventID = uuid.New() }},
		{"event version", func(_ *db.MealCatalogView, d *db.MealDraft) { d.EventVersion++ }},
		{"catalog version", func(_ *db.MealCatalogView, d *db.MealDraft) { d.CatalogVersion++ }},
		{"unsaved", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Version = 0 }},
		{"cleared", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Cleared = true }},
		{"missing evaluation", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Evaluation = nil }},
		{"timezone mismatch", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Timezone = "UTC" }},
		{"local timezone", func(c *db.MealCatalogView, d *db.MealDraft) { c.Timezone = "Local"; d.Timezone = "Local" }},
		{
			"bad timezone",
			func(c *db.MealCatalogView, d *db.MealDraft) { c.Timezone = "Nowhere/Missing"; d.Timezone = c.Timezone },
		},
		{"blank name", func(c *db.MealCatalogView, _ *db.MealDraft) { c.EventName = " " }},
		{
			"changed price",
			func(c *db.MealCatalogView, _ *db.MealDraft) { c.Document.Days[0].Lunch.Combos.WithSoupMinor++ },
		},
		{"altered quote", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Evaluation.Quote.TotalMinor++ }},
		{"altered lines", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Evaluation.Lines[0].AmountMinor++ }},
		{"incomplete", func(_ *db.MealCatalogView, d *db.MealDraft) { d.Evaluation.Choices[0].Lunch.SoupID = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, d := mealTermsFixture(t)
			tc.change(&c, &d)
			_, err := db.FreezeMealDraft(c, d)
			require.ErrorIs(t, err, db.ErrMealTerms)
		})
	}
}

func TestMealTermsRejectOversizedSnapshot(t *testing.T) {
	t.Parallel()
	catalog, draft := mealTermsFixture(t)
	day := &catalog.Document.Days[0]
	day.Lunch = nil
	day.Dinner.Items = nil
	ids := []string{}
	for i := range orders.MaxItems {
		id := fmt.Sprintf("item_%d", i)
		ids = append(ids, id)
		day.Dinner.Items = append(
			day.Dinner.Items,
			orders.MealItem{
				ID:         id,
				Name:       orders.Label{EN: strings.Repeat("A", 128), RU: strings.Repeat("Я", 128)},
				PriceMinor: 1,
			},
		)
	}
	_, err := orders.NormalizeMeals(catalog.Document)
	require.NoError(t, err)
	evaluation, err := orders.EvaluateMeals(catalog.Document, []orders.MealDayChoice{{DayID: day.ID, DinnerIDs: ids}})
	require.NoError(t, err)
	draft.Evaluation = &evaluation
	_, err = db.FreezeMealDraft(catalog, draft)
	require.ErrorIs(t, err, db.ErrMealTerms)
}
