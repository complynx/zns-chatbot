package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
)

func TestRequestScopeStaysInHostContext(t *testing.T) {
	t.Parallel()
	original := t.Context()
	_, exists := agent.RequestScopeFromContext(original)
	assert.False(t, exists)
	want := agent.RequestScope{Owner: "alice", UpdateID: 42, Turn: 1}
	ctx, cancel := context.WithCancel(agent.WithRequestScope(original, want))
	defer cancel()
	got, exists := agent.RequestScopeFromContext(ctx)
	assert.True(t, exists)
	assert.Equal(t, want, got)
	_, exists = agent.RequestScopeFromContext(original)
	assert.False(t, exists)
}
