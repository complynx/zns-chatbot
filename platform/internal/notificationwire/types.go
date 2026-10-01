// Package notificationwire describes the immutable content of a notification
// send. It carries no transport credentials, recipient authority or receipt.
package notificationwire

import (
	"encoding/json"
	"errors"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

const maxPayloadBytes = 64 << 10

var ErrPayload = errors.New("invalid notification wire payload")

// Payload preserves the rendered text and actual inline markup. Domains treat
// the markup as JSON that must decode with the Telegram transport schema.
type Payload struct {
	Text   string          `json:"text"`
	Markup json.RawMessage `json:"markup"`
}

func (p Payload) Encode() ([]byte, error) {
	units := len(utf16.Encode([]rune(p.Text)))
	if !utf8.ValidString(p.Text) || units == 0 || units > 4096 ||
		len(p.Markup) == 0 || p.Markup[0] != '{' || !json.Valid(p.Markup) {
		return nil, ErrPayload
	}
	var markup telegram.Markup
	if json.Unmarshal(p.Markup, &markup) != nil {
		return nil, ErrPayload
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > maxPayloadBytes {
		return nil, ErrPayload
	}
	return encoded, nil
}

// Decode reports presence separately for an absent snapshot. Invalid stored content fails
// closed instead of allowing a new rendering to replace it.
func Decode(encoded []byte) (Payload, bool, error) {
	if len(encoded) == 0 {
		return Payload{}, false, nil
	}
	var p Payload
	if len(encoded) > maxPayloadBytes || json.Unmarshal(encoded, &p) != nil {
		return Payload{}, false, ErrPayload
	}
	if _, err := p.Encode(); err != nil {
		return Payload{}, false, err
	}
	return p, true, nil
}
