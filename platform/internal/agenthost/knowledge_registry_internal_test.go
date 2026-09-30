package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/knowledge"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type knowledgeCatalogCapabilities struct {
	current knowledge.Capabilities
	err     error
	owners  []string
}

func (c *knowledgeCatalogCapabilities) KnowledgeCapabilities(
	_ context.Context, owner string,
) (knowledge.Capabilities, error) {
	c.owners = append(c.owners, owner)
	return c.current, c.err
}

type liveKnowledgeCatalog struct {
	liveScriptCatalog

	knowledge KnowledgeScriptCatalog
}

func (c *liveKnowledgeCatalog) Knowledge(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	return c.knowledge.Entries(ctx, owner)
}

func TestKnowledgeCatalogRetainsRoleSchemasAndConsentBoundary(t *testing.T) {
	t.Parallel()
	for _, role := range []knowledge.Capabilities{
		{}, {CanCurate: true}, {CanReview: true}, {CanCurate: true, CanReview: true},
	} {
		t.Run(roleName(role), func(t *testing.T) {
			t.Parallel()
			client := &knowledgeCatalogCapabilities{current: role}
			entries, err := (KnowledgeScriptCatalog{Client: client, Binding: ScriptToolEntry{ResultLimit: 4096}}).
				Entries(t.Context(), "owner")
			require.NoError(t, err)
			expected := []string{"knowledge.scopes", "knowledge.read", "knowledge.proposals"}
			if role.CanReview {
				expected = append(expected, "knowledge.review_queue")
			}
			expected = append(expected, "knowledge.memos", "knowledge.memo_read", "knowledge.memo_set",
				"knowledge.memo_delete", "knowledge.suggest")
			if role.CanCurate {
				expected = append(expected, "knowledge.curate", "knowledge.remove_fact")
			}
			if role.CanReview {
				expected = append(expected, "knowledge.review_card")
			}
			required := map[string][]string{
				"knowledge.memo_read": {"fact_key"}, "knowledge.memo_delete": {"fact_key"},
				"knowledge.memo_set": {"fact_key", "text"}, "knowledge.suggest": {"topic", "fact_key", "text"},
				"knowledge.curate": {"topic", "fact_key", "text"}, "knowledge.remove_fact": {"topic", "fact_key"},
				"knowledge.review_card": {"proposal_id"},
			}
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 4096, entry.ResultLimit)
				var schema struct {
					Type                 string                     `json:"type"`
					AdditionalProperties bool                       `json:"additionalProperties"`
					Properties           map[string]json.RawMessage `json:"properties"`
					Required             []string                   `json:"required"`
				}
				require.NoError(t, json.Unmarshal(entry.Descriptor.InputSchema, &schema))
				require.Equal(t, "object", schema.Type)
				require.False(t, schema.AdditionalProperties)
				require.Equal(t, required[entry.Descriptor.Name], schema.Required)
				for _, forbidden := range []string{"owner", "source", "key", "version", "decision", "consent"} {
					require.NotContains(t, schema.Properties, forbidden)
				}
			}
			require.Equal(t, expected, names)
			require.NotContains(t, names, "knowledge.submit")
			require.NotContains(t, names, "knowledge.review")
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}

func roleName(role knowledge.Capabilities) string {
	switch {
	case role.CanCurate && role.CanReview:
		return "both"
	case role.CanCurate:
		return "curator"
	case role.CanReview:
		return "reviewer"
	default:
		return "ordinary"
	}
}

func TestKnowledgeCatalogLiveDiscoveryAndKnownNameResolution(t *testing.T) {
	t.Parallel()
	client := &knowledgeCatalogCapabilities{current: knowledge.Capabilities{CanCurate: true}}
	prepared, executed := false, false
	binding := ScriptToolEntry{ResultLimit: 4096,
		Prepare: func(_ context.Context, owner string, update int64, call scriptclient.ToolCall,
			_ agent.Input,
		) (ScriptToolRecord, error) {
			require.Equal(t, "owner", owner)
			require.Equal(t, int64(123), update)
			require.Equal(t, "knowledge.curate", call.Name)
			prepared = true
			return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
		},
		Execute: func(_ context.Context, owner string, call scriptclient.ToolCall, record ScriptToolRecord,
			_ *agent.Input,
		) (any, error) {
			require.Equal(t, "owner", owner)
			require.Equal(t, call.Name, record.Outcome.Name)
			executed = true
			return "domain-result", nil
		},
	}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: &liveKnowledgeCatalog{
		knowledge: KnowledgeScriptCatalog{Client: client, Binding: binding},
	}}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	client.current = knowledge.Capabilities{CanReview: true}
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.NotContains(t, string(list), "knowledge.curate")
	require.NotContains(t, string(list), "knowledge.review_card", "new grants wait for a new VM scope")
	_, err = host.discover(ctx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"knowledge.curate"}`),
	})
	require.Error(t, err)
	_, err = host.Registry.Resolve(ctx, "owner", "knowledge.curate")
	require.Error(t, err, "known-name calls cannot bypass a revoked capability")
	_, err = host.Registry.Resolve(ctx, "owner", "knowledge.review_card")
	require.Error(t, err, "new rights cannot broaden an admitted VM")
	require.False(t, prepared)
	require.False(t, executed)
	require.Equal(t, []string{"owner", "owner", "owner", "owner"}, client.owners)

	client.current.CanCurate = true
	help, err := host.discover(ctx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"knowledge.curate"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "current event curator rights")
	entry, err := host.Registry.Resolve(ctx, "owner", "knowledge.curate")
	require.NoError(t, err)
	call := scriptclient.ToolCall{Name: "knowledge.curate"}
	record, err := entry.Prepare(ctx, "owner", 123, call, agent.Input{})
	require.NoError(t, err)
	result, err := entry.Execute(ctx, "owner", call, record, &agent.Input{})
	require.NoError(t, err)
	require.Equal(t, "domain-result", result)
	require.True(t, prepared)
	require.True(t, executed)
	require.Equal(t, 4096, entry.ResultLimit)
	fresh, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	freshCtx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(fresh))
	_, err = host.Registry.Resolve(freshCtx, "owner", "knowledge.review_card")
	require.NoError(t, err)
}

func TestKnowledgeCatalogFailureExposesNoDescriptors(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, core.DatabaseFailure(errors.New("capability database failure"))} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			client := &knowledgeCatalogCapabilities{current: knowledge.Capabilities{CanCurate: true}, err: failure}
			entries, err := (KnowledgeScriptCatalog{Client: client}).Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, entries)
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}
