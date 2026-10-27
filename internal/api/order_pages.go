package api

import (
	"encoding/json"
	"fmt"

	"github.com/complynx/zns-chatbot/internal/db"
)

// Keep4KiB of the manifest's64KiB response allowance for the protocol envelope.
const ordersPageResultLimit = 60 << 10

// A page is bounded by both count and bytes. Its cursor always names the last
// included order, so a large snapshot cannot skip later retained orders.
func boundedOrdersPage(page db.OrdersPage) (db.OrdersPage, error) {
	result := db.OrdersPage{Orders: []db.SubmittedOrder{}, Next: page.Next}
	for i, order := range page.Orders {
		result.Orders = append(result.Orders, order)
		result.Next = page.Next
		if i < len(page.Orders)-1 {
			result.Next = &order.ID
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return db.OrdersPage{}, err
		}
		if len(raw) <= ordersPageResultLimit {
			continue
		}
		result.Orders = result.Orders[:len(result.Orders)-1]
		if len(result.Orders) == 0 {
			return db.OrdersPage{}, fmt.Errorf("stored order exceeds list response bound")
		}
		result.Next = &result.Orders[len(result.Orders)-1].ID
		return result, nil
	}
	return result, nil
}
