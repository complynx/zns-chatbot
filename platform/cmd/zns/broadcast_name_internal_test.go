package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

type broadcastNameModel struct {
	agent.Scripted

	calls int
}

func (m *broadcastNameModel) InformalName(ctx context.Context, _ map[string]any) (string, error) {
	m.calls++
	return "private-name-canary", ctx.Err()
}

func TestBroadcastOptionsPreservesObservedNameProvider(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	next := &broadcastNameModel{}
	model := observedModel{next: next, runtime: runtime}
	options := broadcastOptions(model)
	require.NotNil(t, options.InformalName)
	name, err := options.InformalName(t.Context(), map[string]any{"legal_name": "private-profile-canary"})
	require.NoError(t, err)
	assert.Equal(t, "private-name-canary", name)
	assert.Equal(t, 1, next.calls)
	response := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	assert.Contains(t, response.Body.String(), `zns_operations_total{operation="model.broadcast_name",result="ok"} 1`)
	assert.NotContains(t, response.Body.String(), "canary")
	assert.Nil(t, broadcastOptions(agent.Scripted{}).InformalName)
	model.next = agent.Scripted{}
	_, err = model.InformalName(t.Context(), nil)
	require.Error(t, err)
}
