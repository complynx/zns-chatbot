package interaction

import (
	"context"
	"errors"
	"net/http"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
)

// AddContext supplies bounded observed orders and the current extra catalog.
func (c OrderCoordinator) AddContext(ctx context.Context, owner string, input *agent.Input, evidence string) error {
	list, err := c.Client.Orders(ctx, owner, c.EventID)
	if err != nil {
		return err
	}
	input.OrderCount = len(list)
	for _, order := range list {
		if order.State == orderStateUnpaid || order.State == orderStateCash {
			input.EditableOrderCount++
		}
	}
	input.Orders = OrderSummaries(list, evidence, input.EditableOrderCount)
	event, err := c.Client.OrderEvent(ctx, owner, c.EventID)
	if problem, ok := errors.AsType[*core.ProblemError](err); ok &&
		problem.Status == http.StatusNotFound && problem.Code == "event_not_found" {
		// Food and pass events can exist without the optional modern orders domain.
		return nil
	}
	if err != nil {
		return err
	}
	input.Extras = event.Extras
	input.OrderHistory, err = c.Client.OrderHistory(ctx, owner, c.EventID)
	return err
}
