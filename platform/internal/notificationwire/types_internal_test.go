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
	p := Payload{Text: "Synthetic text", Markup: json.RawMessage(`{"inline_keyboard":[[{"text":"Open","callback_data":"open:1"}]]}`)}
	encoded, err := p.Encode()
	require.NoError(t, err)
	actual, err := Decode(encoded)
	require.NoError(t, err)
	require.NotNil(t, actual)
	assert.Equal(t, p.Text, actual.Text)
	assert.JSONEq(t, string(p.Markup), string(actual.Markup))
	missing, err := Decode(nil)
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestInvalidPayload(t *testing.T) {
	t.Parallel()
	for _, encoded := range []string{
		`null`, `{}`, `{"text":"Text","markup":null}`, `{"text":"Text","markup":[]}`,
		`{"text":"Text","markup":`, `{"text":"","markup":{}}`,
		`{"text":"` + strings.Repeat("😀", 2049) + `","markup":{}}`,
	} {
		t.Run(encoded[:min(len(encoded), 24)], func(t *testing.T) {
			t.Parallel()
			p, err := Decode([]byte(encoded))
			require.ErrorIs(t, err, ErrPayload)
			assert.Nil(t, p)
		})
	}
}
