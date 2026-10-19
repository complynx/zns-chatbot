package orders_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func catalog() orders.CatalogDocument {
	start := time.Date(2026, time.March, 29, 1, 30, 0, 0, time.UTC)
	return orders.CatalogDocument{Currency: "BYN", CurrencyScale: 2, Items: []orders.Item{
		{ID: "soup", Name: orders.Label{EN: " Soup ", RU: " Суп "}, BasePriceMinor: 1234}},
		Offers: []orders.Offer{{ID: "sunday_lunch", Name: orders.Label{EN: "Lunch", RU: "Обед"},
			StartsAt: start, EndsAt: start.Add(time.Hour), SalesOpensAt: start.Add(-24 * time.Hour),
			SalesClosesAt: start.Add(-time.Minute), ItemIDs: []string{"soup"}}}}
}

func TestCatalogPreservesPricesDatesAndExclusiveSalesBoundary(t *testing.T) {
	t.Parallel()
	d, err := orders.Normalize(catalog())
	require.NoError(t, err)
	require.Equal(t, int64(1234), d.Items[0].BasePriceMinor)
	require.Equal(t, "Суп", d.Items[0].Name.RU)
	offer := d.Offers[0]
	require.False(t, offer.SalesOpen(offer.SalesOpensAt.Add(-time.Microsecond)))
	require.True(t, offer.SalesOpen(offer.SalesOpensAt))
	require.True(t, offer.SalesOpen(offer.SalesClosesAt.Add(-time.Microsecond)))
	require.False(t, offer.SalesOpen(offer.SalesClosesAt))
	zone, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)
	require.Equal(t, "2026-03-29T03:30:00+02:00", offer.StartsAt.In(zone).Format(time.RFC3339))
}

func TestCatalogRejectsAmbiguousReferencesAndInvalidMoneyOrTimes(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*orders.CatalogDocument){
		"unknown item":           func(d *orders.CatalogDocument) { d.Offers[0].ItemIDs = []string{"absent"} },
		"duplicate item":         func(d *orders.CatalogDocument) { d.Items = append(d.Items, d.Items[0]) },
		"duplicate reference":    func(d *orders.CatalogDocument) { d.Offers[0].ItemIDs = []string{"soup", "soup"} },
		"duplicate offer":        func(d *orders.CatalogDocument) { d.Offers = append(d.Offers, d.Offers[0]) },
		"negative price":         func(d *orders.CatalogDocument) { d.Items[0].BasePriceMinor = -1 },
		"unsafe integer":         func(d *orders.CatalogDocument) { d.Items[0].BasePriceMinor = orders.MaxPrice + 1 },
		"missing translation":    func(d *orders.CatalogDocument) { d.Items[0].Name.EN = "" },
		"ambiguous sales window": func(d *orders.CatalogDocument) { d.Offers[0].SalesClosesAt = d.Offers[0].SalesOpensAt },
		"missing date":           func(d *orders.CatalogDocument) { d.Offers[0].StartsAt = time.Time{} },
		"lost precision":         func(d *orders.CatalogDocument) { d.Offers[0].StartsAt = d.Offers[0].StartsAt.Add(time.Nanosecond) },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := catalog()
			change(&d)
			_, err := orders.Normalize(d)
			require.ErrorIs(t, err, orders.ErrInvalid)
		})
	}
}
