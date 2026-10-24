package orders

import (
	"encoding/json"
	"errors"
	"time"
)

const (
	MaxSelections    = 100
	MaxQuantity      = int64(1000)
	MaxTotalQuantity = int64(10000)
	MaxQuoteBytes    = 48 << 10
)

var (
	ErrQuoteUnavailable = errors.New("complete pricing is not configured")
	ErrSelection        = errors.New("invalid order selections")
	ErrSalesClosed      = errors.New("selected offer sales are closed")
	ErrAmount           = errors.New("quote amount exceeds supported bound")
)

type Selection struct {
	OfferID  string `json:"offer_id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	ItemID   string `json:"item_id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Quantity int64  `json:"quantity" minimum:"1" maximum:"1000"`
}

type QuoteLine struct {
	OfferID        string `json:"offer_id"`
	ID             string `json:"id"`
	Name           Label  `json:"name"`
	Quantity       int64  `json:"quantity"`
	UnitPriceMinor int64  `json:"unit_price_minor"`
	AmountMinor    int64  `json:"amount_minor"`
}

type Quote struct {
	Currency              string      `json:"currency"`
	CurrencyScale         int         `json:"currency_scale"`
	Items                 []QuoteLine `json:"items"`
	Supplements           []QuoteLine `json:"supplements"`
	ItemsTotalMinor       int64       `json:"items_total_minor"`
	SupplementsTotalMinor int64       `json:"supplements_total_minor"`
	TotalMinor            int64       `json:"total_minor"`
}

// Calculate uses only canonical, normalized catalog data. It creates no reservation
// or price guarantee; callers must validate current ownership and versions first.
func Calculate(document CatalogDocument, selections []Selection, now time.Time) (Quote, error) {
	d, err := Normalize(document)
	if err != nil {
		return Quote{}, err
	}
	if d.CompletePricing == nil {
		return Quote{}, ErrQuoteUnavailable
	}
	if len(selections) < 1 || len(selections) > MaxSelections {
		return Quote{}, ErrSelection
	}
	result := Quote{Currency: d.Currency, CurrencyScale: d.CurrencyScale,
		Items: make([]QuoteLine, 0, len(selections)), Supplements: []QuoteLine{}}
	items := make(map[string]Item, len(d.Items))
	for _, item := range d.Items {
		items[item.ID] = item
	}
	quantities, err := selectedQuantities(d, selections, openOfferItems(d, now))
	if err != nil {
		return Quote{}, err
	}
	for _, selection := range selections {
		item := items[selection.ItemID]
		line, lineErr := pricedLine(selection.OfferID, item.ID, item.Name, selection.Quantity, item.BasePriceMinor)
		if lineErr != nil {
			return Quote{}, lineErr
		}
		result.Items = append(result.Items, line)
		if err = addAmount(&result.ItemsTotalMinor, line.AmountMinor); err != nil {
			return Quote{}, err
		}
	}
	result.Supplements, result.SupplementsTotalMinor, err = supplementLines(d, quantities)
	if err != nil {
		return Quote{}, err
	}
	result.TotalMinor = result.ItemsTotalMinor
	if err = addAmount(&result.TotalMinor, result.SupplementsTotalMinor); err != nil {
		return Quote{}, err
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > MaxQuoteBytes {
		return Quote{}, ErrSelection
	}
	return result, nil
}

func openOfferItems(d CatalogDocument, now time.Time) map[string]map[string]bool {
	available := make(map[string]map[string]bool, len(d.Offers))
	for _, offer := range d.Offers {
		if !offer.SalesOpen(now) {
			continue
		}
		available[offer.ID] = make(map[string]bool, len(offer.ItemIDs))
		for _, id := range offer.ItemIDs {
			available[offer.ID][id] = true
		}
	}
	return available
}

func supplementLines(d CatalogDocument, quantities map[string]map[string]int64) ([]QuoteLine, int64, error) {
	lines := []QuoteLine{}
	var total int64
	for _, offer := range d.Offers {
		for _, supplement := range d.CompletePricing.Supplements {
			quantity := quantities[offer.ID][supplement.ID]
			if quantity == 0 {
				continue
			}
			line, err := pricedLine(offer.ID, supplement.ID, supplement.Name, quantity, supplement.PriceMinor)
			if err != nil {
				return nil, 0, err
			}
			if err = addAmount(&total, line.AmountMinor); err != nil {
				return nil, 0, err
			}
			lines = append(lines, line)
		}
	}
	return lines, total, nil
}

func selectedQuantities(
	d CatalogDocument,
	selections []Selection,
	available map[string]map[string]bool,
) (map[string]map[string]int64, error) {
	items := make(map[string]Item, len(d.Items))
	for _, item := range d.Items {
		items[item.ID] = item
	}
	modes := make(map[string]ChargeMode)
	for _, supplement := range d.CompletePricing.Supplements {
		modes[supplement.ID] = supplement.Mode
	}
	seen := make(map[[2]string]bool, len(selections))
	result := make(map[string]map[string]int64)
	var total int64
	for _, selection := range selections {
		key := [2]string{selection.OfferID, selection.ItemID}
		if selection.Quantity < 1 || selection.Quantity > MaxQuantity || seen[key] {
			return nil, ErrSelection
		}
		if err := validateSelectedOffer(d, available, selection); err != nil {
			return nil, err
		}
		if !available[selection.OfferID][selection.ItemID] || total > MaxTotalQuantity-selection.Quantity {
			return nil, ErrSelection
		}
		seen[key], total = true, total+selection.Quantity
		if result[selection.OfferID] == nil {
			result[selection.OfferID] = make(map[string]int64)
		}
		for _, id := range items[selection.ItemID].RequiredSupplementIDs {
			if modes[id] == PerOffer {
				result[selection.OfferID][id] = 1
			} else {
				result[selection.OfferID][id] += selection.Quantity
			}
		}
	}
	return result, nil
}

func validateSelectedOffer(d CatalogDocument, available map[string]map[string]bool, selection Selection) error {
	if available[selection.OfferID] != nil {
		return nil
	}
	for _, offer := range d.Offers {
		if offer.ID == selection.OfferID {
			return ErrSalesClosed
		}
	}
	return ErrSelection
}

func pricedLine(offerID, id string, name Label, quantity, price int64) (QuoteLine, error) {
	if price != 0 && quantity > MaxPrice/price {
		return QuoteLine{}, ErrAmount
	}
	return QuoteLine{
		OfferID:        offerID,
		ID:             id,
		Name:           name,
		Quantity:       quantity,
		UnitPriceMinor: price,
		AmountMinor:    quantity * price,
	}, nil
}

func addAmount(total *int64, amount int64) error {
	if amount > MaxPrice-*total {
		return ErrAmount
	}
	*total += amount
	return nil
}
