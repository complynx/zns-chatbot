package scriptclient

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

func TestCallbackAdmissionKeepsDiscoverySeparate(t *testing.T) {
	t.Parallel()
	d := callbackDispatch{allowed: map[string]bool{"orders.get": true}}
	require.False(t, d.admit(ToolCall{Name: "payments.approve"}))
	require.False(t, d.admit(ToolCall{Name: "$help", Arguments: []byte(`{"name":"payments.approve"}`)}))
	for range scriptprotocol.MaxDiscoveries {
		require.True(t, d.admit(ToolCall{Name: "$list", Arguments: []byte(`{}`)}))
	}
	require.False(t, d.admit(ToolCall{Name: "$list", Arguments: []byte(`{}`)}))
	for range scriptprotocol.MaxCalls {
		require.True(t, d.admit(ToolCall{Name: "orders.get", Arguments: []byte(`{}`)}))
	}
	require.False(t, d.admit(ToolCall{Name: "orders.get", Arguments: []byte(`{}`)}))
}
