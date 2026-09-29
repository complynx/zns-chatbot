package agent

import (
	"encoding/json"
	"slices"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

const orderChoicesContextBytes = 8000
const orderCatalogContextBytes = 4000
const orderHistoryContextBytes = 4000

// Project only the provider copy. Host authorization and typed edits retain the
// complete snapshot; omitted details must be read through the bounded tools.
func projectModernOrders(input Input) Input {
	remaining := orderChoicesContextBytes
	input.Orders = slices.Clone(input.Orders)
	incomplete := false
	for index := range input.Orders {
		order := &input.Orders[index]
		body, err := json.Marshal(order.Choice)
		if err != nil || len(body) > remaining {
			order.Choice = orders.Choice{Total: order.Choice.Total}
			order.DetailsIncomplete = true
			incomplete = true
		} else {
			remaining -= len(body)
		}
	}
	if body, err := json.Marshal(input.Extras); err != nil || len(body) > orderCatalogContextBytes {
		input.Extras = nil
		incomplete = true
	}
	if body, err := json.Marshal(input.OrderHistory); err != nil || len(body) > orderHistoryContextBytes {
		input.OrderHistory = compactModernOrderHistory(input.OrderHistory)
		incomplete = true
	}
	if incomplete {
		input.OrderContextNotice = "Order context is incomplete: omitted choices, extras, or history are unknown, not empty. " +
			"Use orders.event for the full catalog, orders.inspect for full order choices, and orders.history.page / " +
			"orders.history.read for history. Continue every paged read until more=false before interpreting it as complete."
	}
	return input
}

func compactModernOrderHistory(history []orders.Change) []orders.Change {
	result := []orders.Change{}
	remaining := orderHistoryContextBytes
	for _, change := range slices.Backward(history) {
		body, err := json.Marshal(change)
		if err != nil || len(body) > remaining {
			change.BeforeExtras, change.Extras, change.Dishes, change.CustomerFields = nil, nil, nil, nil
			body, err = json.Marshal(change)
		}
		if err != nil || len(body) > remaining {
			break
		}
		remaining -= len(body)
		result = append(result, change)
	}
	slices.Reverse(result)
	return result
}
