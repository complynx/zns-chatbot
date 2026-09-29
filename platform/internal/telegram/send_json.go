package telegram

import "encoding/json"

// MarshalJSON clears buttons with an empty keyboard; Telegram rejects null.
func (p Send) MarshalJSON() ([]byte, error) {
	type wireSend Send
	if p.Markup.Rows == nil {
		p.Markup.Rows = [][]Button{}
	}
	return json.Marshal(wireSend(p))
}
