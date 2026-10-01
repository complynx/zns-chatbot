package notificationwire

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPayloadRoundTrip(t *testing.T) {
	t.Parallel()
	p := Payload{
		Text:   "Synthetic text",
		Markup: json.RawMessage(`{"inline_keyboard":[[{"text":"Open","callback_data":"open:1"}]]}`),
	}
	encoded, err := p.Encode()
	require.NoError(t, err)
	actual, present, err := Decode(encoded)
	require.NoError(t, err)
	require.True(t, present)
	assert.Equal(t, p.Text, actual.Text)
	assert.JSONEq(t, string(p.Markup), string(actual.Markup))
	missing, present, err := Decode(nil)
	require.NoError(t, err)
	assert.False(t, present)
	assert.Equal(t, Payload{}, missing)
}

func TestInvalidPayload(t *testing.T) {
	t.Parallel()
	for _, encoded := range []string{
		`null`, `{}`, `{"text":"Text","markup":null}`, `{"text":"Text","markup":[]}`,
		`{"text":"Text","markup":{"inline_keyboard":"invalid"}}`,
		`{"text":"Text","markup":{"inline_keyboard":[[{"text":42}]]}}`,
		`{"text":"Text","markup":{"inline_keyboard":[[{"web_app":"invalid"}]]}}`,
		`{"text":"Text","markup":`, `{"text":"","markup":{}}`,
		`{"text":"` + strings.Repeat("😀", 2049) + `","markup":{}}`,
	} {
		t.Run(encoded[:min(len(encoded), 24)], func(t *testing.T) {
			t.Parallel()
			p, present, err := Decode([]byte(encoded))
			require.ErrorIs(t, err, ErrPayload)
			assert.False(t, present)
			assert.Equal(t, Payload{}, p)
		})
	}
}

func TestPayloadMarkupValidation(t *testing.T) {
	t.Parallel()
	for name, markup := range map[string]string{
		"absent keyboard": `{}`,
		"null keyboard":   `{"inline_keyboard":null}`,
		"empty keyboard":  `{"inline_keyboard":[]}`,
		"callback":        `{"inline_keyboard":[[{"text":"Open","callback_data":"open:1"}]]}`,
		"URL":             `{"inline_keyboard":[[{"text":"Open","url":"https://example.test"}]]}`,
		"web app":         `{"inline_keyboard":[[{"text":"Open","web_app":{"url":"https://example.test"}}]]}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := Payload{Text: "Text", Markup: json.RawMessage(markup)}
			encoded, err := p.Encode()
			require.NoError(t, err)
			actual, present, err := Decode(encoded)
			require.NoError(t, err)
			assert.True(t, present)
			assert.JSONEq(t, markup, string(actual.Markup))
		})
	}
	for name, markup := range map[string]string{
		"null row":           `{"inline_keyboard":[null]}`,
		"null button":        `{"inline_keyboard":[[null]]}`,
		"empty button":       `{"inline_keyboard":[[{}]]}`,
		"missing text":       `{"inline_keyboard":[[{"callback_data":"open"}]]}`,
		"missing action":     `{"inline_keyboard":[[{"text":"Open"}]]}`,
		"null text":          `{"inline_keyboard":[[{"text":null,"callback_data":"open"}]]}`,
		"null action":        `{"inline_keyboard":[[{"text":"Open","callback_data":"open","url":null}]]}`,
		"null web app":       `{"inline_keyboard":[[{"text":"Open","callback_data":"open","web_app":null}]]}`,
		"empty web app":      `{"inline_keyboard":[[{"text":"Open","web_app":{}}]]}`,
		"two actions":        `{"inline_keyboard":[[{"text":"Open","callback_data":"open","url":"https://example.test"}]]}`,
		"unsupported action": `{"inline_keyboard":[[{"text":"Open","callback_data":"open","pay":true}]]}`,
		"unknown markup":     `{"inline_keyboard":[],"resize_keyboard":true}`,
		"unknown web app":    `{"inline_keyboard":[[{"text":"Open","web_app":{"url":"https://example.test","other":true}}]]}`,
		"trailing JSON":      `{"inline_keyboard":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := Payload{Text: "Text", Markup: json.RawMessage(markup)}
			_, err := p.Encode()
			require.ErrorIs(t, err, ErrPayload)
			actual, present, err := Decode([]byte(`{"text":"Text","markup":` + markup + `}`))
			require.ErrorIs(t, err, ErrPayload)
			assert.False(t, present)
			assert.Equal(t, Payload{}, actual)
		})
	}
}
