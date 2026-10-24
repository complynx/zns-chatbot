package orders_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/orders"
)

func capacityCatalog() orders.CatalogDocument {
	d, _ := pricedCatalog()
	d.CompleteCapacity = &orders.CompleteCapacity{Version: 1,
		Resources: []orders.CapacityResource{
			{ID: "seat", Name: orders.Label{EN: " Seat ", RU: " Место "}, Ceiling: 1},
			{ID: "parking", Name: orders.Label{EN: "Parking", RU: "Парковка"}, Ceiling: 0},
		}, Bindings: []orders.CapacityBinding{
			{OfferID: "lunch", ResourceID: "seat", Units: 1},
			{OfferID: "lunch", ItemID: "soup", ResourceID: "seat", Units: 2},
			{OfferID: "lunch", ItemID: "salad", ResourceID: "seat", Units: 3},
			{OfferID: "dinner", ResourceID: "seat", Units: 4},
			{OfferID: "lunch", ResourceID: "parking", Units: 1},
		}}
	return d
}

func TestCapacityRequirementsAddQuantitiesAndDistinctOffersWithoutCheckingAvailability(t *testing.T) {
	t.Parallel()
	d := capacityCatalog()
	rows := []orders.Selection{
		{OfferID: "lunch", ItemID: "soup", Quantity: 2},
		{OfferID: "lunch", ItemID: "salad", Quantity: 3},
		{OfferID: "dinner", ItemID: "soup", Quantity: 9},
	}
	got, err := orders.CapacityRequirements(d, rows)
	require.NoError(t, err)
	require.Equal(t, []orders.ResourceDemand{
		{ResourceID: "parking", Name: orders.Label{EN: "Parking", RU: "Парковка"}, Units: 1},
		{ResourceID: "seat", Name: orders.Label{EN: "Seat", RU: "Место"}, Units: 18},
	}, got.Demands)
	rows[0], rows[2] = rows[2], rows[0]
	again, err := orders.CapacityRequirements(d, rows)
	require.NoError(t, err)
	require.Equal(t, got, again)
}

func TestCapacityUnknownAndExplicitEmptySurviveJSON(t *testing.T) {
	t.Parallel()
	d := catalog()
	rows := []orders.Selection{{OfferID: "sunday_lunch", ItemID: "soup", Quantity: 1}}
	unknown, err := orders.CapacityRequirements(d, rows)
	require.NoError(t, err)
	require.Nil(t, unknown)
	d.CompleteCapacity = &orders.CompleteCapacity{
		Version:   1,
		Resources: []orders.CapacityResource{},
		Bindings:  []orders.CapacityBinding{},
	}
	normalized, err := orders.Normalize(d)
	require.NoError(t, err)
	raw, err := json.Marshal(normalized)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"resources":[],"bindings":[]`)
	var restored orders.CatalogDocument
	require.NoError(t, json.Unmarshal(raw, &restored))
	known, err := orders.CapacityRequirements(restored, rows)
	require.NoError(t, err)
	require.NotNil(t, known)
	require.NotNil(t, known.Demands)
	require.Empty(t, known.Demands)
}

func TestCapacityNormalizationOwnsCanonicalCopies(t *testing.T) {
	t.Parallel()
	d := capacityCatalog()
	before, err := json.Marshal(d)
	require.NoError(t, err)
	normalized, err := orders.Normalize(d)
	require.NoError(t, err)
	after, err := json.Marshal(d)
	require.NoError(t, err)
	require.JSONEq(t, string(before), string(after), "Normalization must not modify caller data")
	require.Equal(t, "parking", normalized.CompleteCapacity.Resources[0].ID)
	normalized.CompleteCapacity.Resources[0].Name.EN = "changed"
	normalized.CompleteCapacity.Bindings[0].Units = 99
	require.Equal(t, "Parking", d.CompleteCapacity.Resources[1].Name.EN)
	require.Equal(t, int64(1), d.CompleteCapacity.Bindings[0].Units)
}

func TestCapacityRejectsAmbiguousOrUnboundedPolicy(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*orders.CompleteCapacity){
		"missing resources":   func(p *orders.CompleteCapacity) { p.Resources = nil },
		"missing bindings":    func(p *orders.CompleteCapacity) { p.Bindings = nil },
		"unknown version":     func(p *orders.CompleteCapacity) { p.Version = 2 },
		"duplicate resource":  func(p *orders.CompleteCapacity) { p.Resources = append(p.Resources, p.Resources[0]) },
		"duplicate binding":   func(p *orders.CompleteCapacity) { p.Bindings = append(p.Bindings, p.Bindings[0]) },
		"foreign offer":       func(p *orders.CompleteCapacity) { p.Bindings[0].OfferID = "missing" },
		"foreign item":        func(p *orders.CompleteCapacity) { p.Bindings[0].ItemID = "missing" },
		"foreign resource":    func(p *orders.CompleteCapacity) { p.Bindings[0].ResourceID = "missing" },
		"zero units":          func(p *orders.CompleteCapacity) { p.Bindings[0].Units = 0 },
		"negative ceiling":    func(p *orders.CompleteCapacity) { p.Resources[0].Ceiling = -1 },
		"oversize ceiling":    func(p *orders.CompleteCapacity) { p.Resources[0].Ceiling = orders.MaxResourceUnits + 1 },
		"oversize units":      func(p *orders.CompleteCapacity) { p.Bindings[0].Units = orders.MaxBindingUnits + 1 },
		"missing translation": func(p *orders.CompleteCapacity) { p.Resources[0].Name.RU = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := capacityCatalog()
			change(d.CompleteCapacity)
			_, err := orders.Normalize(d)
			require.ErrorIs(t, err, orders.ErrInvalid)
		})
	}
}

func TestCapacityRejectsInvalidSelectionsAndAggregateOverflow(t *testing.T) {
	t.Parallel()
	d := capacityCatalog()
	selection := orders.Selection{OfferID: "lunch", ItemID: "soup", Quantity: 1}
	for _, rows := range [][]orders.Selection{
		nil,
		{selection, selection},
		{{OfferID: "missing", ItemID: "soup", Quantity: 1}},
		{{OfferID: "lunch", ItemID: "soup", Quantity: 0}},
	} {
		_, err := orders.CapacityRequirements(d, rows)
		require.ErrorIs(t, err, orders.ErrSelection)
	}
	d.CompleteCapacity.Bindings[1].Units = orders.MaxBindingUnits
	selection.Quantity = orders.MaxQuantity
	_, err := orders.CapacityRequirements(d, []orders.Selection{selection})
	require.ErrorIs(t, err, orders.ErrSelection, "Per-offer contribution plus per-item demand exceeds supported total")
}

func TestCapacityRejectsOversizedCatalogWithoutTruncatingResources(t *testing.T) {
	t.Parallel()
	d := capacityCatalog()
	d.CompleteCapacity.Bindings = []orders.CapacityBinding{}
	d.CompleteCapacity.Resources = nil
	for n := range orders.MaxResources {
		d.CompleteCapacity.Resources = append(
			d.CompleteCapacity.Resources,
			orders.CapacityResource{
				ID:   fmt.Sprintf("resource_%02d", n),
				Name: orders.Label{EN: strings.Repeat("😀", 128), RU: strings.Repeat("😀", 128)},
			},
		)
	}
	_, err := orders.Normalize(d)
	require.ErrorIs(t, err, orders.ErrInvalid)
	require.Len(t, d.CompleteCapacity.Resources, orders.MaxResources)
}
