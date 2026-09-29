package bot

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// Check the domain boundary without spending the VM budget serializing large arrays.
func TestDecodeModernChoiceExtraCountBoundary(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		count int
		valid bool
	}{
		{name: "1024 accepted", count: 1024, valid: true},
		{name: "1025 rejected", count: 1025, valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			input := modernChoiceArguments{
				Operation: "patch",
				Ref:       "51000.0.0",
				Extras:    make([]modernChoiceExtra, test.count),
			}
			for index := range input.Extras {
				input.Extras[index] = modernChoiceExtra{Ref: index, Selected: true}
			}
			encoded, err := json.Marshal(input)
			require.NoError(t, err)
			decoded, err := decodeModernChoice(scriptclient.ToolCall{Name: modernOrdersChoice, Arguments: encoded})
			if !test.valid {
				require.EqualError(t, err, "invalid choice arguments")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, input, decoded)
		})
	}
}
