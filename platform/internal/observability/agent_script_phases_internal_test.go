package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestScriptPhaseOperationsAreExact(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"script.registry.resolve", "script.registry.list", "script.callback", "script.knowledge.refresh", "script.source.admit", "script.call.prepare", "script.call.admit", "script.call.execute", "script.call.complete"} {
		require.Equal(t, name, safeAgentOperation(name))
		require.Equal(t, diagnosticUnknown, safeAgentOperation(name+".private-canary"))
	}
}
