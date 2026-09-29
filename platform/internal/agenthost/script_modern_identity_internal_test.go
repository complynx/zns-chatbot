package agenthost

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestModernReadIdentityKeepsAdmissionBindings(t *testing.T) {
	t.Parallel()
	generation := int64(7)
	admitted := ScriptToolRecord{
		Outcome: agent.ScriptToolResult{Name: "orders.inspect", Error: scriptInterrupted},
		ModernOrder: &ModernOrderRequest{
			ReadOrderID:  "order",
			ReadCursor:   "cursor",
			ReadSnapshot: "snapshot",
			Event:        "event",
			Update:       1,
		},
		Order:  &orders.Command{OrderID: "order", EventID: "event", Version: 3},
		Source: &readsource.Derivation{Generation: &generation},
	}
	identity, err := scriptCallIdentity(admitted)
	require.NoError(t, err)
	changes := map[string]func(*ScriptToolRecord){
		"target":   func(call *ScriptToolRecord) { call.ModernOrder.ReadOrderID = "other" },
		"cursor":   func(call *ScriptToolRecord) { call.ModernOrder.ReadCursor = "other" },
		"snapshot": func(call *ScriptToolRecord) { call.ModernOrder.ReadSnapshot = "other" },
		"version":  func(call *ScriptToolRecord) { call.Order.Version++ },
		"source":   func(call *ScriptToolRecord) { *call.Source.Generation++ },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			altered, cloneErr := cloneScriptCall(admitted)
			require.NoError(t, cloneErr)
			change(&altered)
			changed, identityErr := scriptCallIdentity(altered)
			require.NoError(t, identityErr)
			assert.NotEqual(t, identity, changed)
		})
	}
	replayed, err := cloneScriptCall(admitted)
	require.NoError(t, err)
	replayed.Outcome.Error = ""
	replayIdentity, err := scriptCallIdentity(replayed)
	require.NoError(t, err)
	assert.Equal(t, identity, replayIdentity)
}
