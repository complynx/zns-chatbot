package agenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type policyTestCatalog struct {
	liveScriptCatalog

	models    ModelScriptCatalog
	credits   CreditScriptCatalog
	broadcast BroadcastScriptCatalog
}

func (c *policyTestCatalog) Models(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	if c.models.ReadPermissions == nil {
		return nil, nil
	}
	return c.models.Entries(ctx, owner)
}

func (c *policyTestCatalog) Credits(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	if c.credits.ReadPermissions == nil {
		return nil, nil
	}
	return c.credits.Entries(ctx, owner)
}

func (c *policyTestCatalog) Broadcast(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	if c.broadcast.Allowed == nil {
		return nil, nil
	}
	return c.broadcast.Entries(ctx, owner)
}

type policySchema struct {
	Type                 string                     `json:"type"`
	Properties           map[string]json.RawMessage `json:"properties"`
	Required             []string                   `json:"required"`
	AdditionalProperties bool                       `json:"additionalProperties"`
}

func readPolicySchema(t *testing.T, tool scriptclient.Tool) policySchema {
	t.Helper()
	var schema policySchema
	require.NoError(t, json.Unmarshal(tool.InputSchema, &schema))
	require.Equal(t, "object", schema.Type)
	require.False(t, schema.AdditionalProperties)
	return schema
}

func TestModelCatalogScopeVisibilityOrderAndSchemas(t *testing.T) {
	t.Parallel()
	for _, scope := range []string{"", modelsettings.Own, modelsettings.Others, modelsettings.Global,
		modelsettings.GrantPermission} {
		t.Run(scope, func(t *testing.T) {
			t.Parallel()
			calls := 0
			entries, err := (ModelScriptCatalog{
				ReadPermissions: func(_ context.Context, owner string) (map[string]bool, error) {
					require.Equal(t, "owner", owner)
					calls++
					return map[string]bool{scope: true}, nil
				}, Binding: ScriptToolEntry{ResultLimit: 4 << 10},
			}).Entries(t.Context(), "owner")
			require.NoError(t, err)
			expected := []string{scriptModelsEffective}
			switch scope {
			case modelsettings.Own, modelsettings.Others, modelsettings.Global:
				expected = append(expected, "models."+scope+".get", "models."+scope+".set")
			case modelsettings.GrantPermission:
				expected = append(expected, scriptModelsGrant)
			}
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 4<<10, entry.ResultLimit)
				assertModelPolicySchema(t, entry.Descriptor)
			}
			require.Equal(t, expected, names)
			require.Equal(t, 1, calls)
		})
	}
}

func assertModelPolicySchema(t *testing.T, tool scriptclient.Tool) {
	t.Helper()
	schema := readPolicySchema(t, tool)
	for _, private := range []string{"version", "operation_key", "source", "superadmin"} {
		require.NotContains(t, schema.Properties, private)
	}
	switch tool.Name {
	case scriptModelsEffective, "models.own.get", "models.global.get":
		require.Empty(t, schema.Properties)
		require.Empty(t, schema.Required)
	case "models.others.get":
		require.Equal(t, []string{"owner"}, schema.Required)
		require.JSONEq(t, `{"type":"string","minLength":1,"maxLength":200}`, string(schema.Properties["owner"]))
	case "models.own.set", "models.global.set", "models.others.set":
		require.JSONEq(t, `{"type":"string","maxLength":100}`, string(schema.Properties["model"]))
		require.JSONEq(t, `{"type":"string","maxLength":30}`, string(schema.Properties["effort"]))
		required := []string{"model", "effort"}
		if tool.Name == "models.others.set" {
			required = append([]string{"owner"}, required...)
		}
		require.Equal(t, required, schema.Required)
	case scriptModelsGrant:
		require.Equal(t, []string{"owner", "capability", "enabled"}, schema.Required)
		require.JSONEq(t, `{"enum":["own","others","global"]}`, string(schema.Properties["capability"]))
		require.JSONEq(t, `{"type":"boolean"}`, string(schema.Properties["enabled"]))
	}
}

func TestModelCatalogLiveDiscoveryAndOriginalScope(t *testing.T) {
	t.Parallel()
	permissions := map[string]bool{modelsettings.Own: true}
	calls := 0
	reader := func(_ context.Context, owner string) (map[string]bool, error) {
		require.Equal(t, "owner", owner)
		calls++
		return permissions, nil
	}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: &policyTestCatalog{
		models: ModelScriptCatalog{ReadPermissions: reader},
	}}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	permissions = map[string]bool{modelsettings.Global: true, modelsettings.GrantPermission: true}
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.NotContains(t, string(list), `"name":"models.own.set"`)
	require.NotContains(t, string(list), `"name":"models.global.set"`)
	_, err = host.Registry.Resolve(ctx, "owner", "models.own.set")
	require.Error(t, err)
	_, err = host.Registry.Resolve(ctx, "owner", scriptModelsGrant)
	require.Error(t, err, "new grant cannot broaden a running VM")
	require.Equal(t, 3, calls)
	fresh, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	freshCtx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(fresh))
	help, err := host.discover(freshCtx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"models.grants.set"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "cannot grant superadmin")
	permissions = nil
	_, err = host.Registry.Resolve(freshCtx, "owner", scriptModelsGrant)
	require.Error(t, err)
	require.Equal(t, 6, calls)
}

func TestModelCatalogCompleteOrderAndDomainDenial(t *testing.T) {
	t.Parallel()
	permissions := map[string]bool{modelsettings.Own: true, modelsettings.Others: true,
		modelsettings.Global: true, modelsettings.GrantPermission: true}
	denied := context.Canceled
	prepared, executed := false, false
	client := &policyTestCatalog{models: ModelScriptCatalog{
		ReadPermissions: func(context.Context, string) (map[string]bool, error) { return permissions, nil },
		Binding: ScriptToolEntry{ResultLimit: 4 << 10,
			Prepare: func(_ context.Context, _ string, _ int64, call scriptclient.ToolCall,
				_ agent.Input,
			) (ScriptToolRecord, error) {
				prepared = true
				return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
			},
			Execute: func(context.Context, string, scriptclient.ToolCall, ScriptToolRecord, *agent.Input) (any, error) {
				executed = true
				return nil, denied
			},
		},
	}}
	entries, err := client.models.Entries(t.Context(), "owner")
	require.NoError(t, err)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Descriptor.Name)
	}
	require.Equal(t, []string{scriptModelsEffective, "models.own.get", "models.own.set", "models.others.get",
		"models.others.set", "models.global.get", "models.global.set", scriptModelsGrant}, names)
	entry, err := (ScriptRegistry{Catalog: client}).Resolve(t.Context(), "owner", scriptModelsGrant)
	require.NoError(t, err)
	call := scriptclient.ToolCall{Name: scriptModelsGrant}
	record, err := entry.Prepare(t.Context(), "owner", 1, call, agent.Input{})
	require.NoError(t, err)
	result, err := entry.Execute(t.Context(), "owner", call, record, &agent.Input{})
	require.ErrorIs(t, err, denied)
	require.Nil(t, result)
	require.True(t, prepared)
	require.True(t, executed)
}
