package miniapp

import (
	"context"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/massage"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// Client is the authenticated application boundary used by all enabled web
// routes. Implementations preserve the verified principal in the returned context.
type Client interface {
	AuthenticateTelegram(context.Context, int64) (context.Context, string, error)
	Order(context.Context, string, string, string) (orders.Order, error)
	OrderEvent(context.Context, string, string) (orders.Event, error)
	QuoteOrder(context.Context, string, string, orders.ChoiceInput) (orders.Choice, error)
	ExecuteOrder(context.Context, string, orders.Command) (orders.Order, error)
	MassageTimetable(context.Context, string, string) (massage.Calendar, error)
	FoodView(context.Context, string, string, string) (legacyfood.View, error)
	FoodCommand(context.Context, string, legacyfood.Command) (legacyfood.Order, error)
	FoodQuote(context.Context, string, string, legacyfood.MealSelection) (legacyfood.MealQuote, error)
	FoodLegacyMenu(context.Context, string, string, string, legacyfood.MealSelection) (legacyfood.Order, error)
}
