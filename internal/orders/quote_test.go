package orders_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func pricedCatalog() (orders.CatalogDocument, time.Time) {
	now := time.Date(2026, time.October, 25, 0, 30, 0, 0, time.UTC)
	return orders.CatalogDocument{Currency: "BYN", CurrencyScale: 2,
		Items: []orders.Item{
			{ID: "soup", Name: orders.Label{EN: "Soup", RU: "Суп"}, BasePriceMinor: 100,
				RequiredSupplementIDs: []string{"container", "spoon"}},
			{ID: "salad", Name: orders.Label{EN: "Salad", RU: "Салат"}, BasePriceMinor: 200,
				RequiredSupplementIDs: []string{"container", "spoon"}},
		}, Offers: []orders.Offer{
			{ID: "lunch", Name: orders.Label{EN: "Lunch", RU: "Обед"}, StartsAt: now, EndsAt: now.Add(time.Hour),
				SalesOpensAt: now, SalesClosesAt: now.Add(time.Hour), ItemIDs: []string{"soup", "salad"}},
			{
				ID:            "dinner",
				Name:          orders.Label{EN: "Dinner", RU: "Ужин"},
				StartsAt:      now.Add(3 * time.Hour),
				EndsAt:        now.Add(4 * time.Hour),
				SalesOpensAt:  now,
				SalesClosesAt: now.Add(time.Hour),
				ItemIDs:       []string{"soup", "salad"},
			},
		}, CompletePricing: &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{
			{
				ID:         "container",
				Name:       orders.Label{EN: "Container", RU: "Контейнер"},
				PriceMinor: 10,
				Mode:       orders.PerPortion,
			},
			{ID: "spoon", Name: orders.Label{EN: "Spoon", RU: "Ложка"}, PriceMinor: 5, Mode: orders.PerOffer},
		}}}, now
}

func TestQuoteChargesPortionsAndSharesUtensilsOnlyWithinOffer(t *testing.T) {
	t.Parallel()
	d, now := pricedCatalog()
	quote, err := orders.Calculate(d, []orders.Selection{
		{OfferID: "lunch", ItemID: "soup", Quantity: 2},
		{OfferID: "lunch", ItemID: "salad", Quantity: 1},
		{OfferID: "dinner", ItemID: "soup", Quantity: 1},
	}, now)
	require.NoError(t, err)
	require.Equal(t, int64(500), quote.ItemsTotalMinor)
	require.Equal(t, int64(50), quote.SupplementsTotalMinor)
	require.Equal(t, int64(550), quote.TotalMinor)
	require.Len(t, quote.Supplements, 4)
	require.Equal(t, int64(3), quote.Supplements[0].Quantity)
	require.Equal(t, int64(1), quote.Supplements[1].Quantity)
	require.Equal(t, int64(1), quote.Supplements[2].Quantity)
	require.Equal(t, int64(1), quote.Supplements[3].Quantity)
	_, err = orders.Calculate(
		d,
		[]orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 1}},
		now.Add(time.Hour),
	)
	require.ErrorIs(t, err, orders.ErrSalesClosed)
	_, err = orders.Calculate(
		d,
		[]orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 1}},
		now.Add(-time.Nanosecond),
	)
	require.ErrorIs(t, err, orders.ErrSalesClosed)
	zone, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	_, err = orders.Calculate(d, []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 1}}, now.In(zone))
	require.NoError(t, err, "DST offset does not change the opening instant")
}

func TestQuoteRequiresExplicitCompletePricingAndPreservesOldDocumentBytes(t *testing.T) {
	t.Parallel()
	d, now := pricedCatalog()
	for i := range d.Items {
		d.Items[i].RequiredSupplementIDs = nil
	}
	d.CompletePricing = nil
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "complete_pricing")
	require.NotContains(t, string(raw), "required_supplement_ids")
	selection := []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}}
	_, err = orders.Calculate(d, selection, now)
	require.ErrorIs(t, err, orders.ErrQuoteUnavailable)
	d.CompletePricing = &orders.CompletePricing{Version: 1, Supplements: []orders.Supplement{}}
	quote, err := orders.Calculate(d, selection, now)
	require.NoError(t, err)
	require.Equal(t, int64(200), quote.TotalMinor)
	require.Empty(t, quote.Supplements)
}

func TestQuoteRejectsInvalidSelectionsAndArithmeticBounds(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		selections []orders.Selection
	}{
		{"empty", nil},
		{"zero", []orders.Selection{{OfferID: "lunch", ItemID: "soup"}}},
		{"negative", []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: -1}}},
		{"quantity", []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: orders.MaxQuantity + 1}}},
		{"unknown", []orders.Selection{{OfferID: "lunch", ItemID: "unknown", Quantity: 1}}},
		{"offer", []orders.Selection{{OfferID: "unknown", ItemID: "soup", Quantity: 1}}},
		{
			"duplicate",
			[]orders.Selection{
				{OfferID: "lunch", ItemID: "soup", Quantity: 1},
				{OfferID: "lunch", ItemID: "soup", Quantity: 1},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d, now := pricedCatalog()
			_, err := orders.Calculate(d, tc.selections, now)
			require.ErrorIs(t, err, orders.ErrSelection)
		})
	}
	t.Run("multiplication", func(t *testing.T) {
		t.Parallel()
		d, now := pricedCatalog()
		d.Items[0].BasePriceMinor = orders.MaxPrice
		_, err := orders.Calculate(d, []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}}, now)
		require.ErrorIs(t, err, orders.ErrAmount)
	})
	t.Run("supplement addition", func(t *testing.T) {
		t.Parallel()
		d, now := pricedCatalog()
		d.Items[0].BasePriceMinor = orders.MaxPrice
		_, err := orders.Calculate(d, []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 1}}, now)
		require.ErrorIs(t, err, orders.ErrAmount)
	})
	t.Run("supplement multiplication", func(t *testing.T) {
		t.Parallel()
		d, now := pricedCatalog()
		d.Items[0].BasePriceMinor = 0
		d.CompletePricing.Supplements[0].PriceMinor = orders.MaxPrice
		_, err := orders.Calculate(d, []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 2}}, now)
		require.ErrorIs(t, err, orders.ErrAmount)
	})
	t.Run("item addition", func(t *testing.T) {
		t.Parallel()
		d, now := pricedCatalog()
		d.Items[0].BasePriceMinor, d.Items[1].BasePriceMinor = orders.MaxPrice, orders.MaxPrice
		_, err := orders.Calculate(d, []orders.Selection{{OfferID: "lunch", ItemID: "soup", Quantity: 1},
			{OfferID: "lunch", ItemID: "salad", Quantity: 1}}, now)
		require.ErrorIs(t, err, orders.ErrAmount)
	})
}

func TestQuoteBoundsAggregateQuantityAndResponseWithoutPartialTotal(t *testing.T) {
	t.Parallel()
	d, now := pricedCatalog()
	d.Items, d.Offers[0].ItemIDs = nil, nil
	d.Offers = d.Offers[:1]
	d.CompletePricing.Supplements = []orders.Supplement{}
	selections := make([]orders.Selection, 0, 100)
	for i := range 20 {
		id := fmt.Sprintf("item%d", i)
		d.Items = append(d.Items, orders.Item{ID: id, Name: orders.Label{
			EN: strings.Repeat(
				"a",
				orders.MaxLabelRunes,
			),
			RU: strings.Repeat("я", orders.MaxLabelRunes),
		}, BasePriceMinor: 1})
		d.Offers[0].ItemIDs = append(d.Offers[0].ItemIDs, id)
		selections = append(selections, orders.Selection{OfferID: "lunch", ItemID: id, Quantity: orders.MaxQuantity})
	}
	_, err := orders.Calculate(d, selections[:11], now)
	require.ErrorIs(t, err, orders.ErrSelection, "Total portions must stay bounded")
	for i := 1; i < 5; i++ {
		offer := d.Offers[0]
		offer.ID = fmt.Sprintf("offer%d", i)
		d.Offers = append(d.Offers, offer)
	}
	selections = selections[:0]
	for _, offer := range d.Offers {
		for _, item := range d.Items {
			selections = append(selections, orders.Selection{OfferID: offer.ID, ItemID: item.ID, Quantity: 1})
		}
	}
	_, err = orders.Normalize(d)
	require.NoError(t, err, "Catalog fits; repeated output labels may exceed the quote byte limit")
	quote, err := orders.Calculate(d, selections, now)
	require.ErrorIs(t, err, orders.ErrSelection)
	require.Zero(t, quote.TotalMinor, "Oversized output must not masquerade as a partial total")
}

func TestCompletePricingValidationAndCopy(t *testing.T) {
	t.Parallel()
	d, _ := pricedCatalog()
	normalized, err := orders.Normalize(d)
	require.NoError(t, err)
	normalized.Items[0].RequiredSupplementIDs[0] = "changed"
	normalized.CompletePricing.Supplements[0].Name.EN = "Changed"
	require.Equal(t, "container", d.Items[0].RequiredSupplementIDs[0])
	require.Equal(t, "Container", d.CompletePricing.Supplements[0].Name.EN)
	mutations := []func(*orders.CatalogDocument){
		func(d *orders.CatalogDocument) { d.CompletePricing.Version = 2 },
		func(d *orders.CatalogDocument) { d.CompletePricing.Supplements = nil },
		func(d *orders.CatalogDocument) { d.CompletePricing.Supplements[0].Mode = "unknown" },
		func(d *orders.CatalogDocument) { d.CompletePricing.Supplements[0].PriceMinor = -1 },
		func(d *orders.CatalogDocument) { d.CompletePricing.Supplements[1].ID = "container" },
		func(d *orders.CatalogDocument) { d.Items[0].RequiredSupplementIDs = []string{"missing"} },
		func(d *orders.CatalogDocument) { d.Items[0].RequiredSupplementIDs = []string{"container", "container"} },
		func(d *orders.CatalogDocument) { d.CompletePricing.Supplements[0].Name.EN = strings.Repeat("a", 129) },
	}
	for _, mutate := range mutations {
		invalid, _ := pricedCatalog()
		mutate(&invalid)
		_, invalidErr := orders.Normalize(invalid)
		require.ErrorIs(t, invalidErr, orders.ErrInvalid)
	}
}
