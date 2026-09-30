package agenthost

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/modelsettings"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestCreditCatalogRoleSchemasAndBounds(t *testing.T) {
	t.Parallel()
	for _, admin := range []bool{false, true} {
		t.Run("role", func(t *testing.T) {
			t.Parallel()
			calls := 0
			entries, err := (CreditScriptCatalog{
				ReadPermissions: func(_ context.Context, owner string) (map[string]bool, error) {
					require.Equal(t, "owner", owner)
					calls++
					return map[string]bool{"admin": admin, modelsettings.GrantPermission: true}, nil
				}, Binding: ScriptToolEntry{ResultLimit: 32 << 10},
			}).Entries(t.Context(), "owner")
			require.NoError(t, err)
			expected := []string{scriptCreditUsage, scriptCreditHistory}
			if admin {
				expected = append(expected, scriptCreditAdminUsage, scriptCreditAdminHistory,
					scriptCreditDefault, scriptCreditPolicy)
			}
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 32<<10, entry.ResultLimit)
				assertCreditPolicySchema(t, entry.Descriptor)
			}
			require.Equal(t, expected, names)
			require.Equal(t, 1, calls)
		})
	}
}

func assertCreditPolicySchema(t *testing.T, tool scriptclient.Tool) {
	t.Helper()
	schema := readPolicySchema(t, tool)
	require.NotContains(t, schema.Properties, "owner")
	require.NotContains(t, schema.Properties, "operation_key")
	switch tool.Name {
	case scriptCreditUsage, scriptCreditDefault:
		require.Empty(t, schema.Properties)
	case scriptCreditHistory, scriptCreditAdminHistory:
		require.JSONEq(t, `{"type":"string","maxLength":2048}`, string(schema.Properties["cursor"]))
	case scriptCreditPolicy:
		require.Equal(t, []string{"payer", "amount", "version"}, schema.Required)
		require.JSONEq(t, `{"type":"string","maxLength":30}`, string(schema.Properties["amount"]))
		require.JSONEq(t, `{"type":"integer","minimum":1,"maximum":9007199254740991}`,
			string(schema.Properties["version"]))
	}
	if tool.Name == scriptCreditAdminUsage || tool.Name == scriptCreditAdminHistory || tool.Name == scriptCreditPolicy {
		require.JSONEq(t, `{"type":"string","maxLength":256}`, string(schema.Properties["payer"]))
	}
}

func TestCreditCatalogLiveRoleRefreshAndDomainDenial(t *testing.T) {
	t.Parallel()
	admin, calls := true, 0
	denied := &core.ProblemError{Status: 403, Code: "administrator_revoked"}
	executed := false
	client := &policyTestCatalog{credits: CreditScriptCatalog{
		ReadPermissions: func(_ context.Context, owner string) (map[string]bool, error) {
			require.Equal(t, "owner", owner)
			calls++
			return map[string]bool{"admin": admin}, nil
		}, Binding: ScriptToolEntry{ResultLimit: 32 << 10,
			Prepare: func(_ context.Context, _ string, _ int64, call scriptclient.ToolCall,
				_ agent.Input,
			) (ScriptToolRecord, error) {
				return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
			},
			Execute: func(context.Context, string, scriptclient.ToolCall, ScriptToolRecord, *agent.Input) (any, error) {
				executed = true
				return nil, denied
			},
		},
	}}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: client}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	admin = false
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Contains(t, string(list), `"name":"credits.usage"`)
	require.NotContains(t, string(list), `"name":"credits.admin.policy"`)
	_, err = host.Registry.Resolve(ctx, "owner", scriptCreditPolicy)
	require.Error(t, err)
	require.False(t, executed)
	admin = true
	help, err := host.discover(ctx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"credits.admin.policy"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "future admissions, not past charges")
	entry, err := host.Registry.Resolve(ctx, "owner", scriptCreditPolicy)
	require.NoError(t, err)
	call := scriptclient.ToolCall{Name: scriptCreditPolicy}
	record, err := entry.Prepare(ctx, "owner", 1, call, agent.Input{})
	require.NoError(t, err)
	result, err := entry.Execute(ctx, "owner", call, record, &agent.Input{})
	require.ErrorIs(t, err, denied)
	require.Nil(t, result)
	require.True(t, executed)
	require.Equal(t, 5, calls)
}

func TestPolicyCatalogReadFailuresHaveNoPartialVisibility(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded,
		core.DatabaseFailure(&core.ProblemError{Status: 403, Code: "forbidden"})} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			calls := 0
			reader := func(_ context.Context, owner string) (map[string]bool, error) {
				require.Equal(t, "owner", owner)
				calls++
				return map[string]bool{"admin": true, modelsettings.GrantPermission: true}, failure
			}
			models, err := (ModelScriptCatalog{ReadPermissions: reader}).Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, models)
			credits, err := (CreditScriptCatalog{ReadPermissions: reader}).Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, credits)
			broadcast, err := (BroadcastScriptCatalog{Allowed: func(context.Context, string) (bool, error) {
				calls++
				return true, failure
			}}).Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, broadcast)
			require.Equal(t, 3, calls)
		})
	}
}
