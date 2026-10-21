package db

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/internal/db/sqlc"
	"github.com/complynx/zns-chatbot/internal/orders"
)

var (
	ErrEmptyMealOrder      = errors.New("meal order has no selections")
	ErrMealDeadlineMissing = errors.New("meal payment deadline is not configured")
)

func (s *Store) orderSnapshotForKind(ctx context.Context, tx pgx.Tx, scope ProfileScope,
	args OrderSubmission, kind OrderKind,
) (OrderSnapshot, time.Time, error) {
	if kind == OrderKindService {
		return s.orderSnapshot(ctx, tx, scope, args)
	}
	if kind != OrderKindMeal {
		return OrderSnapshot{}, time.Time{}, ErrConflict
	}
	q := s.q.WithTx(tx)
	event, err := q.LockRegistrationEvent(ctx, sqlc.LockRegistrationEventParams{
		ID: args.EventID, BotID: scope.BotID, OrganizationID: scope.Organization,
	})
	if err != nil {
		return OrderSnapshot{}, time.Time{}, err
	}
	catalog, err := readMealCatalog(ctx, q, args.EventID, 0)
	if err != nil {
		return OrderSnapshot{}, time.Time{}, err
	}
	draft, err := readMealDraft(ctx, q, scope.UserID, args.EventID)
	if err != nil {
		return OrderSnapshot{}, time.Time{}, err
	}
	now, err := q.RegistrationNow(ctx)
	if err != nil {
		return OrderSnapshot{}, time.Time{}, err
	}
	view := MealCatalogView{MealCatalog: catalog, EventVersion: event.Version,
		EventName: event.Name, Timezone: event.Timezone, ObservedAt: now.Time.UTC()}
	snapshot, err := buildMealOrderSnapshot(view, draft, args)
	return snapshot, view.ObservedAt, err
}

// buildMealOrderSnapshot freezes only a complete, currently saleable selection.
// Unlimited quantities are explicit empty demands, never an absent capacity policy.
func buildMealOrderSnapshot(catalog MealCatalogView, draft MealDraft, args OrderSubmission) (OrderSnapshot, error) {
	if catalog.EventID != args.EventID || catalog.EventVersion != args.ExpectedEventVersion ||
		catalog.Version != args.ExpectedCatalogVersion || draft.Version != args.ExpectedDraftVersion || draft.Version == math.MaxInt64 ||
		draft.EventVersion != catalog.EventVersion || draft.CatalogVersion != catalog.Version {
		return OrderSnapshot{}, ErrConflict
	}
	terms, err := FreezeMealDraft(catalog, draft)
	if err != nil {
		return OrderSnapshot{}, err
	}
	if len(terms.Lines) == 0 {
		return OrderSnapshot{}, ErrEmptyMealOrder
	}
	if terms.PaymentDeadline == nil {
		return OrderSnapshot{}, ErrMealDeadlineMissing
	}
	if !catalog.ObservedAt.Before(*terms.PaymentDeadline) || !selectedMealServicesOpen(terms, catalog.ObservedAt) {
		return OrderSnapshot{}, orders.ErrSalesClosed
	}
	return OrderSnapshot{
		Meal: &terms, EventName: terms.EventName, Selections: []orders.Selection{},
		Capacity:      &orders.CapacitySnapshot{Version: 1, Demands: []orders.ResourceDemand{}},
		SalesClosesAt: *terms.PaymentDeadline,
		Quote: OrdersQuoteView{Quote: mealOrderQuote(terms), EventID: terms.EventID,
			EventVersion: terms.EventVersion, Version: terms.CatalogVersion,
			Timezone: terms.Timezone, ObservedAt: catalog.ObservedAt},
	}, nil
}

func selectedMealServicesOpen(terms MealTerms, now time.Time) bool {
	for _, day := range terms.Days {
		for _, service := range []*MealServiceTerms{day.Lunch, day.Dinner} {
			if service != nil && len(service.Items) > 0 &&
				(now.Before(service.SalesOpensAt) || !now.Before(service.SalesClosesAt)) {
				return false
			}
		}
	}
	return true
}

// mealOrderQuote retains one monetary basket line. Detailed names and charges
// live in MealTerms, avoiding duplicate descriptions in the bounded receipt.
func mealOrderQuote(terms MealTerms) orders.Quote {
	total := terms.Quote.TotalMinor
	return orders.Quote{
		Currency: terms.Quote.Currency, CurrencyScale: terms.Quote.CurrencyScale,
		Items: []orders.QuoteLine{{OfferID: orderModuleMeals, ID: "composition",
			Name: orders.Label{EN: "Meals", RU: "Питание"}, Quantity: 1, UnitPriceMinor: total, AmountMinor: total}},
		Supplements: []orders.QuoteLine{}, ItemsTotalMinor: total, TotalMinor: total,
	}
}
