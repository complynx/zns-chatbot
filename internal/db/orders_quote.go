package db

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/complynx/zns-chatbot/internal/orders"
)

type OrdersQuoteArguments struct {
	EventID              uuid.UUID          `json:"event_id"`
	ExpectedEventVersion int64              `json:"expected_event_version" minimum:"1"`
	ExpectedVersion      int64              `json:"expected_version"       minimum:"1"`
	Selections           []orders.Selection `json:"selections"                         minItems:"1" maxItems:"100"`
}

type OrdersQuoteView struct {
	orders.Quote

	EventID      uuid.UUID `json:"event_id"`
	EventVersion int64     `json:"event_version"`
	Version      int64     `json:"version"`
	Timezone     string    `json:"timezone"`
	ObservedAt   time.Time `json:"observed_at"`
}

// OwnOrdersQuote calculates from the same authorized snapshot as the catalog
// read. No second connection, durable receipt or business mutation is involved.
func (s *Store) OwnOrdersQuote(
	ctx context.Context,
	scope ProfileScope,
	args OrdersQuoteArguments,
) (OrdersQuoteView, error) {
	snapshot, err := s.OwnOrdersCatalog(ctx, scope, args.EventID)
	if err != nil {
		return OrdersQuoteView{}, err
	}
	if args.ExpectedEventVersion != snapshot.EventVersion || args.ExpectedVersion != snapshot.Version {
		return OrdersQuoteView{}, ErrConflict
	}
	document := orders.CatalogDocument{
		Currency:         snapshot.Currency,
		CurrencyScale:    snapshot.CurrencyScale,
		Items:            snapshot.Items,
		CompletePricing:  snapshot.CompletePricing,
		CompleteCapacity: snapshot.CompleteCapacity,
		Offers:           make([]orders.Offer, 0, len(snapshot.Offers)),
	}
	for _, offer := range snapshot.Offers {
		document.Offers = append(document.Offers, offer.Offer)
	}
	quote, err := orders.Calculate(document, args.Selections, snapshot.ObservedAt)
	if err != nil {
		return OrdersQuoteView{}, err
	}
	return OrdersQuoteView{Quote: quote, EventID: snapshot.EventID, EventVersion: snapshot.EventVersion,
		Version: snapshot.Version, Timezone: snapshot.Timezone, ObservedAt: snapshot.ObservedAt}, nil
}
