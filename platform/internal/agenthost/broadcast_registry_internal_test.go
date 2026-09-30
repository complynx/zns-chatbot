package agenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestBroadcastCatalogWholeFamilyAndManualConsentSchemas(t *testing.T) {
	t.Parallel()
	for _, allowed := range []bool{false, true} {
		t.Run("allowed", func(t *testing.T) {
			t.Parallel()
			calls := 0
			catalog := BroadcastScriptCatalog{
				Allowed: func(_ context.Context, owner string) (bool, error) {
					require.Equal(t, "owner", owner)
					calls++
					return allowed, nil
				}, ActionBinding: ScriptToolEntry{ResultLimit: 4 << 10},
				ReviewBinding: ScriptToolEntry{ResultLimit: 32 << 10},
				ReadBinding:   ScriptToolEntry{ResultLimit: 32 << 10},
			}
			entries, err := catalog.Entries(t.Context(), "owner")
			require.NoError(t, err)
			require.Equal(t, 1, calls)
			if !allowed {
				require.Nil(t, entries)
				return
			}
			names := []string{scriptBroadcastPreview, scriptBroadcastPending, scriptBroadcastAttach,
				scriptBroadcastCancel, scriptBroadcastAudience, scriptBroadcastProfile,
				scriptBroadcastReview, scriptBroadcastShow}
			require.Len(t, entries, len(names))
			for index, entry := range entries {
				require.Equal(t, names[index], entry.Descriptor.Name)
				assertBroadcastPolicySchema(t, entry)
			}
		})
	}
}

func assertBroadcastPolicySchema(t *testing.T, entry ScriptToolEntry) {
	t.Helper()
	schema := readPolicySchema(t, entry.Descriptor)
	for _, private := range []string{"owner", "chat", "message", "source", "key", "confirm", "send"} {
		require.NotContains(t, schema.Properties, private)
	}
	limit := 4 << 10
	switch entry.Descriptor.Name {
	case scriptBroadcastPreview:
		require.Equal(t, []string{"command"}, schema.Required)
		require.JSONEq(t, `{"type":"string","maxLength":16384}`, string(schema.Properties["command"]))
		require.Contains(t, entry.Descriptor.Description, "manual confirmation button")
	case scriptBroadcastPending:
		require.Empty(t, schema.Properties)
	case scriptBroadcastAttach, scriptBroadcastCancel:
		require.Equal(t, []string{"input_id"}, schema.Required)
		require.JSONEq(t, `{"type":"integer","minimum":1}`, string(schema.Properties["input_id"]))
	case scriptBroadcastAudience, scriptBroadcastProfile:
		limit = 32 << 10
		require.JSONEq(t, `{"type":"string","maxLength":2048}`, string(schema.Properties["cursor"]))
		if entry.Descriptor.Name == scriptBroadcastProfile {
			require.Equal(t, []string{"user_id"}, schema.Required)
			require.JSONEq(t, `{"type":"string","maxLength":20}`, string(schema.Properties["user_id"]))
		} else {
			require.Empty(t, schema.Required)
			require.Len(t, schema.Properties, 1)
		}
	case scriptBroadcastReview, scriptBroadcastShow:
		limit = 32 << 10
		require.Equal(t, []string{"id"}, schema.Required)
		require.JSONEq(t, `{"type":"integer","minimum":1}`, string(schema.Properties["id"]))
		require.JSONEq(t, `{"type":"integer","minimum":0}`, string(schema.Properties["offset"]))
		if entry.Descriptor.Name == scriptBroadcastReview {
			require.JSONEq(t, `{"type":"string","maxLength":2048}`, string(schema.Properties["cursor"]))
		} else {
			require.NotContains(t, schema.Properties, "cursor")
			require.Contains(t, entry.Descriptor.Description, "separate manual Send confirmation")
		}
	}
	require.Equal(t, limit, entry.ResultLimit)
}

func TestBroadcastCatalogLivePermissionAndSeparateDispatch(t *testing.T) {
	t.Parallel()
	allowed, calls := true, 0
	denied := &core.ProblemError{Status: 403, Code: "broadcast_permission_revoked"}
	var prepared, executed string
	binding := func(group string, limit int) ScriptToolEntry {
		return ScriptToolEntry{ResultLimit: limit,
			Prepare: func(_ context.Context, owner string, update int64, call scriptclient.ToolCall,
				_ agent.Input,
			) (ScriptToolRecord, error) {
				require.Equal(t, "owner", owner)
				require.Equal(t, int64(123), update)
				prepared = group
				return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
			},
			Execute: func(context.Context, string, scriptclient.ToolCall, ScriptToolRecord, *agent.Input) (any, error) {
				executed = group
				return nil, denied
			},
		}
	}
	client := &policyTestCatalog{broadcast: BroadcastScriptCatalog{
		Allowed: func(_ context.Context, owner string) (bool, error) {
			require.Equal(t, "owner", owner)
			calls++
			return allowed, nil
		}, ActionBinding: binding("action", 4<<10), ReviewBinding: binding("review", 32<<10),
		ReadBinding: binding("read", 32<<10),
	}}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: client}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	allowed = false
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.NotContains(t, string(list), `"name":"broadcasts.preview"`)
	_, err = host.Registry.Resolve(ctx, "owner", scriptBroadcastShow)
	require.Error(t, err)
	require.Empty(t, prepared)
	allowed = true
	help, err := host.discover(ctx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"broadcasts.show"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "never enqueues or sends the campaign")
	for _, test := range []struct{ name, group string }{
		{name: scriptBroadcastPreview, group: "action"}, {name: scriptBroadcastShow, group: "review"},
		{name: scriptBroadcastProfile, group: "read"},
	} {
		entry, resolveErr := host.Registry.Resolve(ctx, "owner", test.name)
		require.NoError(t, resolveErr)
		call := scriptclient.ToolCall{Name: test.name}
		record, prepareErr := entry.Prepare(ctx, "owner", 123, call, agent.Input{})
		require.NoError(t, prepareErr)
		result, executeErr := entry.Execute(ctx, "owner", call, record, &agent.Input{})
		require.ErrorIs(t, executeErr, denied)
		require.Nil(t, result)
		require.Equal(t, test.group, prepared)
		require.Equal(t, test.group, executed)
	}
	require.Equal(t, 7, calls)
	_, err = host.Registry.Resolve(ctx, "owner", "broadcasts.send")
	require.Error(t, err)
	_, err = host.Registry.Resolve(ctx, "owner", "broadcasts.confirm")
	require.Error(t, err)
	require.Equal(t, 7, calls, "unbound send/confirm names cannot trigger another capability read")
}
