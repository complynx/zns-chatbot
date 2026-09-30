package legacyfood

import (
	"context"
	"encoding/json"

	"github.com/complynx/zns-chatbot/platform/internal/core"
)

type View struct {
	Event        Event             `json:"event"`
	Order        Order             `json:"order"`
	Instructions map[string]string `json:"instructions"`
}

func (s Service) Quote(ctx context.Context, actor, event string, meals MealSelection) (MealQuote, error) {
	return s.QuoteObserved(ctx, actor, event, meals, "")
}

func (s Service) QuoteObserved(
	ctx context.Context,
	actor, event string,
	meals MealSelection,
	revision string,
) (MealQuote, error) {
	catalog, err := s.Event(ctx, actor, event)
	if err != nil {
		return MealQuote{}, err
	}
	if err = checkCatalogRevision(catalog, revision); err != nil {
		return MealQuote{}, err
	}
	var menu Menu
	if err = json.Unmarshal(catalog.Menu, &menu); err != nil {
		return MealQuote{}, err
	}
	return QuoteMeals(menu, catalog.MealPrices, meals)
}

// ReviewView is distinct from the owner-only legacy menu route.
func (s Service) ReviewView(ctx context.Context, actor, event, id string) (View, error) {
	var view View
	var err error
	view.Event, err = s.Event(ctx, actor, event)
	if err != nil {
		return view, err
	}
	if err = adminAllowed(ctx, s.DB, actor, event, "review"); err != nil {
		return view, err
	}
	view.Order, err = s.Get(ctx, actor, event, id)
	return view, err
}

func (s Service) View(ctx context.Context, actor, event, id string) (View, error) {
	var view View
	var err error
	if event == "" {
		view.Event, err = s.CurrentEvent(ctx, actor)
	} else {
		view.Event, err = s.Event(ctx, actor, event)
	}
	if err != nil {
		return view, err
	}
	if id != "" && len(id) == 24 {
		view.Order, err = s.resolveSourceOrder(ctx, actor, id)
	} else {
		view.Order, err = loadOrder(ctx, s.DB, view.Event.ID, id, actor)
	}
	if noOrder(err) && id == "" {
		view.Order = Order{
			EventID:    view.Event.ID,
			Owner:      actor,
			Meals:      MealSelection{},
			Activities: Activities{},
			MealPayment: Payment{
				Kind:   Meals,
				Status: Pending,
			},
			ActivityPayment: Payment{Kind: Activity, Status: Pending},
		}
		err = nil
	}
	if err != nil {
		return view, err
	}
	if view.Order.Owner != actor || view.Order.EventID != view.Event.ID {
		return View{}, forbidden()
	}
	if view.Order.PaymentAdmin != "" {
		var raw json.RawMessage
		err = s.DB.QueryRow(ctx, `SELECT instructions FROM core.food_admins WHERE event_id=$1 AND owner=$2`, view.Event.ID, view.Order.PaymentAdmin).
			Scan(&raw)
		if noOrder(err) {
			return view, nil
		}
		if err != nil {
			return view, core.DatabaseOperationError(err)
		}
		if err = json.Unmarshal(raw, &view.Instructions); err != nil {
			return view, err
		}
	}
	return view, nil
}
