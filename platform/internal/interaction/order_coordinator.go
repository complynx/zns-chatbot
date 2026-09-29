package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

// OrderClient contains only the user operations consumed by order coordination.
type OrderClient interface {
	Orders(context.Context, string, string) ([]orders.Order, error)
	OrderEvent(context.Context, string, string) (orders.Event, error)
	OrderHistory(context.Context, string, string) ([]orders.Change, error)
	Order(context.Context, string, string, string) (orders.Order, error)
	ExecuteOrder(context.Context, string, orders.Command) (orders.Order, error)
}

// DerivedOrderClient accepts source evidence from the trusted planner host.
type DerivedOrderClient interface {
	ExecuteDerivedOrder(context.Context, string, orders.Command, readsource.Derivation) (orders.Order, error)
}

// OrderCoordinator owns command execution and durable expected refusals.
// The client verifies the principal and the domain checks live authority before replay.
type OrderCoordinator struct {
	Client  OrderClient
	Host    DerivedOrderClient
	Store   Store
	EventID string
}

type OrderOutcome struct {
	Order   orders.Order
	Refusal *core.ProblemError
}

// Execute derives the same key for manual and agent mutations. A retry always
// reaches the domain, so a recorded outcome never bypasses current authorization.
func (c OrderCoordinator) Execute(
	ctx context.Context,
	owner string,
	updateID int64,
	command orders.Command,
) (OrderOutcome, error) {
	command.Key = fmt.Sprintf("tg-order-%d", updateID)
	updated, err := c.Client.ExecuteOrder(ctx, owner, command)
	return c.outcome(ctx, owner, updateID, updated, err)
}

// ExecuteDerived retains the saved source evidence through the domain commit.
func (c OrderCoordinator) ExecuteDerived(ctx context.Context, owner string, updateID int64,
	command orders.Command, source readsource.Derivation) (OrderOutcome, error) {
	command.Key = fmt.Sprintf("tg-order-%d", updateID)
	updated, err := c.Host.ExecuteDerivedOrder(ctx, owner, command, source.Clone())
	return c.outcome(ctx, owner, updateID, updated, err)
}

func (c OrderCoordinator) outcome(ctx context.Context, owner string, updateID int64,
	updated orders.Order, err error) (OrderOutcome, error) {
	if err == nil {
		return OrderOutcome{Order: updated}, nil
	}
	problem, ok := errors.AsType[*core.ProblemError](err)
	if !ok || problem.Status >= http.StatusInternalServerError {
		return OrderOutcome{}, err
	}
	raw, err := json.Marshal(problem)
	if err != nil {
		return OrderOutcome{}, err
	}
	_, err = c.Store.DB.Exec(ctx, `INSERT INTO bot.interactions(owner,update_id,kind,content)
 VALUES($1,$2,'order_error',$3) ON CONFLICT DO NOTHING`, owner, updateID, raw)
	if err != nil {
		return OrderOutcome{}, err
	}
	return OrderOutcome{Refusal: problem}, nil
}

// RecordRefusal preserves the durable canonical outcome of an authorized receipt denial.
// It never executes a command or substitutes a missing-receipt result.
func (c OrderCoordinator) RecordRefusal(ctx context.Context, owner string, updateID int64,
	problem *core.ProblemError) (OrderOutcome, error) {
	return c.outcome(ctx, owner, updateID, orders.Order{}, problem)
}
