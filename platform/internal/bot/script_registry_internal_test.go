package bot

import (
	"encoding/json"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

func TestScriptCatalogSize(t *testing.T) {
	t.Parallel()
	b := &Bot{}
	tools := append(scriptTools(true), memoryTools()...)
	entries := append(b.scriptProfileEntries(), b.scriptReadEntries()...)
	entries = append(entries, b.scriptDomainEntries()...)
	for _, entry := range entries {
		tools = append(tools, entry.Descriptor)
	}
	encoded, err := json.Marshal(tools)
	require.NoError(t, err)
	names := make([]scriptclient.Tool, 0, len(tools))
	for _, tool := range tools {
		names = append(names, scriptclient.Tool{Name: tool.Name})
	}
	bindings, err := json.Marshal(names)
	require.NoError(t, err)
	require.NoError(t, scriptprotocol.ValidateTools(tools))
	type summary struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	list := make([]summary, 0, len(tools))
	for _, tool := range tools {
		list = append(list, summary{Name: tool.Name, Description: tool.Description})
	}
	listed, err := json.Marshal(list)
	require.NoError(t, err)
	t.Logf(
		"tools=%d full_descriptors=%d binding_descriptors=%d list=%d",
		len(tools),
		len(encoded),
		len(bindings),
		len(listed),
	)
}

func TestReadCursorCannotCrossOwnerOperationOrResource(t *testing.T) {
	t.Parallel()
	original := scriptReadCursor{Owner: "alice", Kind: "orders.read", Scope: "order-one", Offset: 6000, Version: 2}
	encoded := encodeScriptCursor(original)
	for _, target := range []scriptReadCursor{
		{Owner: "bob", Kind: original.Kind, Scope: original.Scope},
		{Owner: original.Owner, Kind: "history.page", Scope: original.Scope},
		{Owner: original.Owner, Kind: original.Kind, Scope: "order-two"},
	} {
		_, err := readScriptCursor(encoded, target.Owner, target.Kind, target.Scope)
		require.Error(t, err)
	}
	decoded, err := readScriptCursor(encoded, original.Owner, original.Kind, original.Scope)
	require.NoError(t, err)
	require.Equal(t, original, decoded)
}

func TestScriptReadProjectionPreservesFailureEvidence(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"history.page", "history.read", "orders.page", "orders.read"} {
		successful := agent.ScriptToolResult{
			Name:   name,
			Result: json.RawMessage(`{"private":"large intermediate read"}`),
		}
		projected := agenthost.ScriptCallProjection(successful)
		require.Contains(t, string(projected.Result), "payload_omitted")
		require.NotContains(t, string(projected.Result), "large intermediate read")
		successful.Error = "interrupted"
		require.Equal(t, successful, agenthost.ScriptCallProjection(successful))
	}
}
