package agent

// FoodHint is a current owner-authorized payment prompt, never an instruction
// to consume the next message. Receipt selection remains explicit media input.
type FoodHint struct {
	EventID string `json:"event_id"`
	OrderID string `json:"order_id"`
	Kind    string `json:"kind"`
	State   string `json:"state"`
}
