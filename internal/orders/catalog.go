// Package orders defines event-bound catalogs and exact read-only calculations.
package orders

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxItems         = 100
	MaxOffers        = 20
	MaxDocumentBytes = 48 << 10
	MaxPrice         = int64(9_000_000_000_000_000)
	MaxCurrencyScale = 6
	MaxLabelRunes    = 128
)

var ErrInvalid = errors.New("invalid orders catalog")
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
var currency = regexp.MustCompile(`^[A-Z]{3}$`)

type Label struct {
	EN string `json:"en" minLength:"1" maxLength:"128"`
	RU string `json:"ru" minLength:"1" maxLength:"128"`
}

type Item struct {
	ID                    string   `json:"id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name                  Label    `json:"name"`
	BasePriceMinor        int64    `json:"base_price_minor" minimum:"0" maximum:"9000000000000000"`
	RequiredSupplementIDs []string `json:"required_supplement_ids,omitempty" maxItems:"20" uniqueItems:"true"`
}

type Offer struct {
	ID            string    `json:"id" pattern:"^[a-z][a-z0-9_-]{0,31}$"`
	Name          Label     `json:"name"`
	StartsAt      time.Time `json:"starts_at"`
	EndsAt        time.Time `json:"ends_at"`
	SalesOpensAt  time.Time `json:"sales_opens_at"`
	SalesClosesAt time.Time `json:"sales_closes_at"`
	ItemIDs       []string  `json:"item_ids" minItems:"1" maxItems:"100" uniqueItems:"true"`
}

type CatalogDocument struct {
	Currency         string            `json:"currency" pattern:"^[A-Z]{3}$"`
	CurrencyScale    int               `json:"currency_scale" minimum:"0" maximum:"6"`
	Items            []Item            `json:"items" minItems:"1" maxItems:"100"`
	Offers           []Offer           `json:"offers" minItems:"1" maxItems:"20"`
	CompletePricing  *CompletePricing  `json:"complete_pricing,omitempty"`
	CompleteCapacity *CompleteCapacity `json:"complete_capacity,omitempty"`
}

// Normalize copies and validates the catalog, retaining explicit absolute times.
// References are scoped to this document; weekday names never determine dates.
func Normalize(d CatalogDocument) (CatalogDocument, error) {
	if !currency.MatchString(d.Currency) || d.CurrencyScale < 0 || d.CurrencyScale > MaxCurrencyScale ||
		len(d.Items) < 1 || len(d.Items) > MaxItems || len(d.Offers) < 1 || len(d.Offers) > MaxOffers {
		return CatalogDocument{}, ErrInvalid
	}
	d.Items = append([]Item(nil), d.Items...)
	d.Offers = append([]Offer(nil), d.Offers...)
	pricing, pricingErr := normalizePricing(d.CompletePricing)
	if pricingErr != nil {
		return CatalogDocument{}, pricingErr
	}
	d.CompletePricing = pricing
	items := make(map[string]bool, len(d.Items))
	for i, item := range d.Items {
		if !identifier.MatchString(item.ID) || items[item.ID] || item.BasePriceMinor < 0 ||
			item.BasePriceMinor > MaxPrice {
			return CatalogDocument{}, ErrInvalid
		}
		label, err := normalizeLabel(item.Name)
		if err != nil {
			return CatalogDocument{}, err
		}
		d.Items[i].Name = label
		refs, refErr := normalizeSupplementReferences(item.RequiredSupplementIDs, pricing)
		if refErr != nil {
			return CatalogDocument{}, refErr
		}
		d.Items[i].RequiredSupplementIDs = refs
		items[item.ID] = true
	}
	offers := make(map[string]bool, len(d.Offers))
	for i, offer := range d.Offers {
		if !identifier.MatchString(offer.ID) || offers[offer.ID] {
			return CatalogDocument{}, ErrInvalid
		}
		normalized, err := normalizeOffer(offer, items)
		if err != nil {
			return CatalogDocument{}, err
		}
		d.Offers[i] = normalized
		offers[offer.ID] = true
	}
	capacity, err := normalizeCapacity(d)
	if err != nil {
		return CatalogDocument{}, err
	}
	d.CompleteCapacity = capacity
	raw, err := json.Marshal(d)
	if err != nil || len(raw) > MaxDocumentBytes {
		return CatalogDocument{}, ErrInvalid
	}
	return d, nil
}

func normalizeLabel(label Label) (Label, error) {
	label.EN, label.RU = strings.TrimSpace(label.EN), strings.TrimSpace(label.RU)
	for _, value := range []string{label.EN, label.RU} {
		if !utf8.ValidString(value) || value == "" || utf8.RuneCountInString(value) > MaxLabelRunes ||
			strings.ContainsAny(value, "\x00\r\n") {
			return Label{}, ErrInvalid
		}
	}
	return label, nil
}

func normalizeOffer(offer Offer, items map[string]bool) (Offer, error) {
	label, err := normalizeLabel(offer.Name)
	if err != nil {
		return Offer{}, err
	}
	if !validInstant(offer.StartsAt) || !validInstant(offer.EndsAt) ||
		!validInstant(offer.SalesOpensAt) || !validInstant(offer.SalesClosesAt) ||
		!offer.EndsAt.After(offer.StartsAt) || !offer.SalesClosesAt.After(offer.SalesOpensAt) ||
		len(offer.ItemIDs) < 1 || len(offer.ItemIDs) > MaxItems {
		return Offer{}, ErrInvalid
	}
	seen := make(map[string]bool, len(offer.ItemIDs))
	for _, id := range offer.ItemIDs {
		if !items[id] || seen[id] {
			return Offer{}, ErrInvalid
		}
		seen[id] = true
	}
	offer.Name, offer.ItemIDs = label, append([]string(nil), offer.ItemIDs...)
	offer.StartsAt, offer.EndsAt = offer.StartsAt.UTC(), offer.EndsAt.UTC()
	offer.SalesOpensAt, offer.SalesClosesAt = offer.SalesOpensAt.UTC(), offer.SalesClosesAt.UTC()
	return offer, nil
}

func validInstant(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Nanosecond()%int(time.Microsecond) == 0
}

// SalesOpen uses an inclusive opening and exclusive closing instant. It does not
// assert entitlement, capacity or that order submission is implemented.
func (o Offer) SalesOpen(now time.Time) bool {
	return !now.Before(o.SalesOpensAt) && now.Before(o.SalesClosesAt)
}
