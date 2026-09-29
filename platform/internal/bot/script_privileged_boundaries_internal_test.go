package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestPrivilegedReadArgumentByteBoundaries(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name, event, party, cursor string
		valid                      bool
	}{
		{"escaped-maximum", strings.Repeat(`"`, 200), strings.Repeat(`\`, 200), "", true},
		{"unicode-maximum", strings.Repeat("Ж", 100), strings.Repeat("🌍", 50), "", true},
		{"event-over", strings.Repeat("x", 201), "", "", false},
		{"unicode-event-over", strings.Repeat("Ж", 101), "", "", false},
		{"party-over", "event", strings.Repeat("x", 201), "", false},
		{"cursor-maximum", "event", "", strings.Repeat("x", 2048), true},
		{"cursor-over", "event", "", strings.Repeat("x", 2049), false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			args, err := json.Marshal(
				scriptPrivilegedArguments{Event: scenario.event, Party: scenario.party, Cursor: scenario.cursor},
			)
			require.NoError(t, err)
			_, err = prepareScriptPrivilegedRead(
				t.Context(),
				"alice",
				1,
				scriptclient.ToolCall{Name: scriptPractitionerBookings, Arguments: args},
				agent.Input{},
			)
			if scenario.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
