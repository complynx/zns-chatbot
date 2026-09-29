package agenthost

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestRetiredMemoryProjectionRejectsUnrelatedFailures(t *testing.T) {
	t.Parallel()
	stale := errors.New("stale")
	denied := errors.New("permission denied")
	for _, failure := range []error{nil, denied, errors.Join(stale, denied),
		errors.Join(stale, context.Canceled), fmt.Errorf("wrapped: %w", errors.Join(stale, denied))} {
		assert.False(t, onlyScriptStale(failure, stale))
	}
	assert.True(t, onlyScriptStale(errors.Join(fmt.Errorf("wrapped: %w", stale)), stale))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	host := ScriptHost{Store: ScriptStore{StaleError: stale}}
	projected, err := host.projectRetiredMemory(ctx, "alice", 1, 0, &agent.Input{}, stale)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, projected, "cancelled context must not read or project the ledger")
}

func TestRetiredMemoryProjectionRequiresMemoryRetirement(t *testing.T) {
	t.Parallel()
	interrupted := agent.ScriptRun{Error: scriptInterrupted}
	assert.True(t, memoryRetirementProjection(ScriptRecord{MemoryRedacted: true, Run: interrupted}))
	assert.False(t, memoryRetirementProjection(ScriptRecord{PassRedacted: true, Run: interrupted}))
	assert.False(
		t,
		memoryRetirementProjection(ScriptRecord{MemoryRedacted: true, HistoryRedacted: true, Run: interrupted}),
	)
	assert.False(t, memoryRetirementProjection(ScriptRecord{MemoryRedacted: true}))
	assert.False(t, memoryRetirementProjection(ScriptRecord{Run: interrupted}))
}
