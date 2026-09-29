package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

// ErrOrderReadUnavailable identifies a retryable failure while binding an observed order.
var ErrOrderReadUnavailable = errors.New("order binding read unavailable")

const orderOriginAgent = "agent"
const orderActionCreate = "create"
const orderActionEdit = "edit"

const orderStateCash = "cash"
const orderStateUnpaid = "unpaid"

// OrderSummaries keeps recent summaries and any explicitly named order. Full choices are loaded
// only when executing a version-bound proposal, never copied from model context.
func OrderSummaries(list []orders.Order, text string, editableCount int) []agent.OrderSummary {
	const recentOrders = 5
	const maxSummaries = 10
	result := []agent.OrderSummary{}
	for index, order := range slices.Backward(list) {
		singleEditable := editableCount == 1 && (order.State == orderStateUnpaid || order.State == orderStateCash)
		if index < len(list)-recentOrders && !strings.Contains(text, order.ID) && !singleEditable {
			continue
		}
		result = append(result, agent.OrderSummary{ID: order.ID, Version: order.Version, State: order.State,
			Choice: orders.Choice{Extras: order.Choice.Extras, Total: order.Choice.Total}})
		if len(result) == maxSummaries {
			break
		}
	}
	return result
}

func (c OrderCoordinator) Bind(
	ctx context.Context,
	owner string,
	proposal *agent.OrderProposal,
	input agent.Input,
	evidence string,
) (*orders.Command, error) {
	command := &orders.Command{EventID: c.EventID, Origin: orderOriginAgent}
	if proposal.Name == orders.ActionPaymentInstructions {
		return c.instructions(proposal, input, evidence)
	}
	if proposal.Name == "export" {
		command.Name = "export"
		return command, nil
	}
	if proposal.Name == orderActionCreate {
		command.Name, command.Choice = orderActionCreate, &orders.ChoiceInput{}
		return command, nil
	}
	extra, exists := input.Extras[proposal.Extra]
	if !exists || extra.Legacy {
		return nil, errors.New("unknown order extra")
	}
	if input.EditableOrderCount > 1 && !strings.Contains(evidence, proposal.OrderID) {
		return nil, errors.New("explicit order selection required")
	}
	for _, order := range input.Orders {
		if order.ID != proposal.OrderID {
			continue
		}
		if order.State != "unpaid" && order.State != orderStateCash {
			return nil, errors.New("order is not editable")
		}
		current, err := c.Client.Order(ctx, owner, c.EventID, order.ID)
		if err != nil {
			return nil, orderBindingError(err)
		}
		if current.Version != order.Version {
			return nil, errors.New("order changed while planning")
		}
		choice := current.Choice.Input()
		if proposal.Name == "remove_extra" {
			delete(choice.Extras, proposal.Extra)
		} else {
			choice.Extras[proposal.Extra] = json.RawMessage(`0`)
		}
		command.Name, command.OrderID, command.Version, command.Choice = orderActionEdit, order.ID, order.Version, &choice
		return command, nil
	}
	return nil, errors.New("unknown order")
}

func (c OrderCoordinator) instructions(
	proposal *agent.OrderProposal,
	input agent.Input,
	evidence string,
) (*orders.Command, error) {
	if input.OrderCount != 1 && !strings.Contains(evidence, proposal.OrderID) {
		return nil, errors.New("explicit order selection required")
	}
	return &orders.Command{
		EventID: c.EventID,
		Origin:  orderOriginAgent,
		Name:    orders.ActionPaymentInstructions,
		OrderID: proposal.OrderID,
	}, nil
}

func orderBindingError(err error) error {
	problem, expected := errors.AsType[*core.ProblemError](err)
	if !expected || problem.Status >= http.StatusInternalServerError {
		return fmt.Errorf("%w: %w", ErrOrderReadUnavailable, err)
	}
	return err
}
