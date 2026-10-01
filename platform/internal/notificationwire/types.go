// Package notificationwire describes the immutable content of a notification
// send. It carries no transport credentials, recipient authority or receipt.
package notificationwire

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
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
	decoder := json.NewDecoder(bytes.NewReader(p.Markup))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&markup) != nil || !validMarkupNulls(p.Markup) {
		return nil, ErrPayload
	}
	for _, row := range markup.Rows {
		for _, button := range row {
			if !validButton(button) {
				return nil, ErrPayload
			}
		}
	}
	encoded, err := json.Marshal(p)
	if err != nil || len(encoded) > maxPayloadBytes {
		return nil, ErrPayload
	}
	return encoded, nil
}

func validButton(button telegram.Button) bool {
	if button.Text == "" {
		return false
	}
	actions := 0
	if button.Data != "" {
		actions++
	}
	if button.URL != "" {
		actions++
	}
	if button.WebApp != nil {
		if button.WebApp.URL == "" {
			return false
		}
		actions++
	}
	return actions == 1
}

// Only an absent keyboard may be null. Nested nulls would become zero values
// when the transport decodes and reserializes the captured markup.
func validMarkupNulls(raw []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	depth := 0
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return true
		}
		if err != nil || (token == nil && depth > 1) {
			return false
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
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
