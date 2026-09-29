package bot

import (
	"encoding/json"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestPrivilegedReadArgumentsAndProjection(t *testing.T) {
	t.Parallel()
	for _, name := range []string{scriptPrivilegeEvents, scriptPaymentQueue, scriptPaymentHistory, scriptPractitionerSchedule, scriptPractitionerPreferences, scriptPractitionerBookings} {
		for _, raw := range []string{`{"event":"a","owner":"other"}`, `{"event":"a","provider":"other"}`, `{"event":"a","view":"timetable"}`} {
			_, err := prepareScriptPrivilegedRead(
				t.Context(),
				"actor",
				1,
				scriptclient.ToolCall{Name: name, Arguments: json.RawMessage(raw)},
				agent.Input{},
			)
			require.Error(t, err)
		}
		outcome := agent.ScriptToolResult{Name: name, Result: json.RawMessage(`{"private":"participant canary"}`)}
		projected := agenthost.ScriptCallProjection(outcome)
		assert.NotContains(t, string(projected.Result), "participant canary")
		assert.Contains(t, string(projected.Result), "payload_omitted")
		count, empty := agenthost.ScriptToolResultMetadata(
			name,
			json.RawMessage(`{"items":[{"private":"participant canary"}],"more":true}`),
		)
		if name == scriptPractitionerPreferences {
			assert.Zero(t, count)
		} else {
			assert.Equal(t, 1, count)
		}
		assert.False(t, empty)
	}
}
