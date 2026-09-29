package adminmessage

import "github.com/complynx/zns-chatbot/platform/internal/delivery"

// Completion records one exact attempt; transport cooldown is not a failure budget.
type Completion struct {
	ID      int64            `json:"id"`
	Attempt int64            `json:"attempt"`
	Outcome delivery.Outcome `json:"outcome"`
}
