package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/passbooking"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type registrationCatalogCapabilities struct {
	current passbooking.ToolCapabilities
	err     error
	owners  []string
}

func (c *registrationCatalogCapabilities) PassToolCapabilities(
	_ context.Context, owner string,
) (passbooking.ToolCapabilities, error) {
	c.owners = append(c.owners, owner)
	return c.current, c.err
}

type liveRegistrationCatalog struct {
	liveScriptCatalog

	registration RegistrationScriptCatalog
}

func (c *liveRegistrationCatalog) Passes(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	return c.registration.Entries(ctx, owner)
}

func TestRegistrationCatalogDiscoveryUnionAndCancelMapping(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name         string
		capabilities passbooking.ToolCapabilities
		extra        []string
	}{
		{name: "ordinary"},
		{name: "assign", capabilities: passbooking.ToolCapabilities{Actions: []string{"admin_assign"}},
			extra: []string{"passes.admin.assign", "passes.admin.cancel", "passes.admin.queue",
				"passes.admin.target", "passes.batch.assign"}},
		{name: "cancel", capabilities: passbooking.ToolCapabilities{Actions: []string{"admin_cancel"}},
			extra: []string{"passes.batch.cancel", "passes.tiers"}},
		{name: "review", capabilities: passbooking.ToolCapabilities{Actions: []string{"proof_accept"}},
			extra: []string{"passes.payments.accept", "passes.payments.review"}},
		{name: "export", capabilities: passbooking.ToolCapabilities{Export: true}, extra: []string{"passes.export"}},
		{name: "unknown", capabilities: passbooking.ToolCapabilities{Actions: []string{"forged_action"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &registrationCatalogCapabilities{current: test.capabilities}
			entries, err := (RegistrationScriptCatalog{Client: client}).Entries(t.Context(), "owner")
			require.NoError(t, err)
			expected := append([]string{"passes.registration.read", "passes.registration.show",
				"passes.operations", "passes.resume"}, test.extra...)
			slices.Sort(expected)
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 32<<10, entry.ResultLimit)
			}
			require.Equal(t, expected, names)
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}

func TestRegistrationCatalogRetainsSchemasAndPrivateAuthorityExclusion(t *testing.T) {
	t.Parallel()
	actions := []string{}
	for _, action := range PassToolActions() {
		actions = append(actions, action)
	}
	client := &registrationCatalogCapabilities{current: passbooking.ToolCapabilities{Actions: actions, Export: true}}
	entries, err := (RegistrationScriptCatalog{Client: client}).Entries(t.Context(), "owner")
	require.NoError(t, err)
	require.Len(t, entries, len(PassToolActions())+5)
	for _, entry := range entries {
		var schema struct {
			Type                 string                     `json:"type"`
			AdditionalProperties bool                       `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             []string                   `json:"required"`
		}
		require.NoError(t, json.Unmarshal(entry.Descriptor.InputSchema, &schema))
		require.Equal(t, "object", schema.Type)
		require.False(t, schema.AdditionalProperties)
		for _, forbidden := range []string{"owner", "source", "key", "version", "proof", "proof_id", "chat"} {
			require.NotContains(t, schema.Properties, forbidden)
		}
		switch entry.Descriptor.Name {
		case scriptPassExport:
			require.Empty(t, schema.Properties)
			require.Empty(t, schema.Required)
		case scriptPassResume:
			require.Equal(t, []string{"operation_id"}, schema.Required)
			require.Len(t, schema.Properties, 1)
		case scriptPassOperations:
			require.Empty(t, schema.Required)
			require.Len(t, schema.Properties, 1)
		case scriptPassShow:
			require.NotContains(t, schema.Properties, "cursor")
			require.Equal(t, []string{"event"}, schema.Required)
		case scriptPassRead:
			require.Contains(t, schema.Properties, "cursor")
		case scriptPassBatchAssign, scriptPassBatchCancel, scriptRegistrationBatchUncouple:
			require.Equal(t, []string{"event", "recipients"}, schema.Required)
			var recipients struct {
				Min    int  `json:"minItems"`
				Max    int  `json:"maxItems"`
				Unique bool `json:"uniqueItems"`
			}
			require.NoError(t, json.Unmarshal(schema.Properties["recipients"], &recipients))
			require.Equal(t, 1, recipients.Min)
			require.Equal(t, passbooking.MaxAdminBatchRecipients, recipients.Max)
			require.True(t, recipients.Unique)
		}
		if assignment, ok := schema.Properties["assignment"]; ok {
			var fields struct {
				AdditionalProperties bool                       `json:"additionalProperties"`
				Properties           map[string]json.RawMessage `json:"properties"`
			}
			require.NoError(t, json.Unmarshal(assignment, &fields))
			require.False(t, fields.AdditionalProperties)
			require.Len(t, fields.Properties, 9)
			require.Contains(t, fields.Properties, "from_profile")
			require.Contains(t, fields.Properties, "legal_name")
			require.NotContains(t, fields.Properties, "owner")
			require.NotContains(t, fields.Properties, "version")
			require.NotContains(t, fields.Properties, "key")
		}
	}
}

func TestRegistrationCatalogLiveListHelpResolveAndDomainCallbacks(t *testing.T) {
	t.Parallel()
	client := &registrationCatalogCapabilities{current: passbooking.ToolCapabilities{Actions: []string{"admin_assign"}}}
	prepared, executed := false, false
	binding := ScriptToolEntry{
		Prepare: func(_ context.Context, owner string, update int64, call scriptclient.ToolCall,
			_ agent.Input,
		) (ScriptToolRecord, error) {
			require.Equal(t, "owner", owner)
			require.Equal(t, int64(123), update)
			prepared = true
			return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
		},
		Execute: func(_ context.Context, owner string, call scriptclient.ToolCall, record ScriptToolRecord,
			_ *agent.Input,
		) (any, error) {
			require.Equal(t, "owner", owner)
			require.Equal(t, call.Name, record.Outcome.Name)
			executed = true
			return nil, &core.ProblemError{Status: 403, Code: "event_scope_forbidden"}
		},
	}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: &liveRegistrationCatalog{
		registration: RegistrationScriptCatalog{Client: client, Binding: binding},
	}}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	client.current = passbooking.ToolCapabilities{Export: true}
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.NotContains(t, string(list), "passes.admin.cancel")
	require.NotContains(t, string(list), "passes.export")
	_, err = host.discover(ctx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"passes.admin.cancel"}`),
	})
	require.Error(t, err)
	_, err = host.Registry.Resolve(ctx, "owner", "passes.admin.cancel")
	require.Error(t, err)
	_, err = host.Registry.Resolve(ctx, "owner", "passes.export")
	require.Error(t, err, "new grants cannot broaden an admitted VM")
	require.False(t, prepared)
	require.False(t, executed)
	require.Len(t, client.owners, 4)
	client.current.Actions = []string{"admin_assign"}
	entry, err := host.Registry.Resolve(ctx, "owner", "passes.admin.assign")
	require.NoError(t, err)
	call := scriptclient.ToolCall{Name: "passes.admin.assign"}
	record, err := entry.Prepare(ctx, "owner", 123, call, agent.Input{})
	require.NoError(t, err)
	result, err := entry.Execute(ctx, "owner", call, record, &agent.Input{})
	var problem *core.ProblemError
	require.ErrorAs(t, err, &problem)
	require.Equal(t, "event_scope_forbidden", problem.Code, "discovery never grants event-scoped execution")
	require.Nil(t, result)
	require.True(t, prepared)
	require.True(t, executed)
	fresh, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	freshCtx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(fresh))
	_, err = host.Registry.Resolve(freshCtx, "owner", "passes.export")
	require.NoError(t, err)
	client.current.Export = false
	_, err = host.Registry.Resolve(freshCtx, "owner", "passes.export")
	require.Error(t, err)
}

func TestRegistrationCatalogCapabilityFailureHasNoProjection(t *testing.T) {
	t.Parallel()
	failure := core.DatabaseFailure(errors.New("capability query"))
	client := &registrationCatalogCapabilities{current: passbooking.ToolCapabilities{Export: true}, err: failure}
	entries, err := (RegistrationScriptCatalog{Client: client}).Entries(t.Context(), "owner")
	require.ErrorIs(t, err, core.ErrDatabase)
	require.Nil(t, entries)
	require.Equal(t, []string{"owner"}, client.owners)
}
