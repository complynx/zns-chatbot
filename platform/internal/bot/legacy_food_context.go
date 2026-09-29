package bot

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func (b *Bot) addFoodHint(ctx context.Context, owner string, input *agent.Input) error {
	input.Food = nil
	var command legacyfood.Command
	err := b.DB.QueryRow(ctx, `SELECT command FROM bot.food_pending WHERE owner=$1 AND expires_at>now()`, owner).
		Scan(&command)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	view, err := b.API.FoodView(ctx, owner, command.EventID, command.OrderID)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok && problem.Status < http.StatusInternalServerError {
		return nil
	}
	if err != nil {
		return err
	}
	payment := view.Order.MealPayment
	if command.Kind == legacyfood.Activity {
		payment = view.Order.ActivityPayment
	}
	if view.Order.Version != command.Version || payment.Generation != command.Generation || payment.Locked() {
		return nil
	}
	input.Food = &agent.FoodHint{
		EventID: view.Event.ID,
		OrderID: view.Order.ID,
		Kind:    payment.Kind,
		State:   payment.Status,
	}
	return nil
}
