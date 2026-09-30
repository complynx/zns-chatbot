package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type foodCatalogCapabilities struct {
	current legacyfood.OwnerCapabilities
	failure error
	owners  []string
}

func (c *foodCatalogCapabilities) FoodCapabilities(
	_ context.Context, owner string,
) (legacyfood.OwnerCapabilities, error) {
	c.owners = append(c.owners, owner)
	return c.current, c.failure
}

type liveFoodCatalog struct {
	liveScriptCatalog

	food FoodScriptCatalog
}

func (c *liveFoodCatalog) Food(ctx context.Context, owner string) ([]ScriptToolEntry, error) {
	return c.food.Entries(ctx, owner)
}

func TestFoodCatalogWholeFamilyRequiresActiveEventAndCurrentRoles(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		capability legacyfood.OwnerCapabilities
		expected   []string
	}{
		{name: "no-event", capability: legacyfood.OwnerCapabilities{CanReview: true, CanExport: true}},
		{name: "owner", capability: legacyfood.OwnerCapabilities{EventID: "event"},
			expected: []string{scriptFoodView, scriptFoodQuote, scriptFoodChange, scriptFoodPrepare}},
		{name: "reviewer", capability: legacyfood.OwnerCapabilities{EventID: "event", CanReview: true},
			expected: []string{scriptFoodView, scriptFoodQuote, scriptFoodChange, scriptFoodPrepare,
				scriptFoodReviewQueue, scriptFoodReviewRead, scriptFoodReviewDecide, scriptFoodReviewProof}},
		{name: "exporter", capability: legacyfood.OwnerCapabilities{EventID: "event", CanExport: true},
			expected: []string{scriptFoodView, scriptFoodQuote, scriptFoodChange, scriptFoodPrepare, scriptFoodExport}},
		{name: "both", capability: legacyfood.OwnerCapabilities{EventID: "event", CanReview: true, CanExport: true},
			expected: []string{scriptFoodView, scriptFoodQuote, scriptFoodChange, scriptFoodPrepare,
				scriptFoodReviewQueue, scriptFoodReviewRead, scriptFoodReviewDecide, scriptFoodReviewProof, scriptFoodExport}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			client := &foodCatalogCapabilities{current: test.capability}
			catalog := FoodScriptCatalog{
				Client: client,
				OwnerBinding: ScriptToolEntry{
					ResultLimit: 32 << 10,
				},
				AdminBinding: ScriptToolEntry{ResultLimit: 32 << 10},
			}
			entries, err := catalog.Entries(t.Context(), "owner")
			require.NoError(t, err)
			var names []string
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 32<<10, entry.ResultLimit)
			}
			require.Equal(t, test.expected, names)
			require.Equal(t, []string{"owner"}, client.owners)
			if test.capability.EventID == "" {
				require.Nil(t, entries)
			}
		})
	}
}

func TestFoodCatalogRetainsSchemasAndHostPaymentIdentity(t *testing.T) {
	t.Parallel()
	client := &foodCatalogCapabilities{current: legacyfood.OwnerCapabilities{
		EventID: "event", CanReview: true, CanExport: true,
	}}
	entries, err := (FoodScriptCatalog{Client: client}).Entries(t.Context(), "owner")
	require.NoError(t, err)
	required := map[string][]string{
		scriptFoodQuote: {"meals"}, scriptFoodChange: {"name"}, scriptFoodPrepare: {"kind"},
		scriptFoodReviewRead: {"order_id"}, scriptFoodReviewDecide: {"kind", "decision"},
		scriptFoodReviewProof: {"kind"},
	}
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
		require.Equal(t, required[entry.Descriptor.Name], schema.Required)
		for _, forbidden := range []string{"owner", "event", "version", "generation", "key", "source", "proof", "url"} {
			require.NotContains(t, schema.Properties, forbidden)
		}
		switch entry.Descriptor.Name {
		case scriptFoodView:
			var cursor struct {
				MaxLength int `json:"maxLength"`
			}
			require.NoError(t, json.Unmarshal(schema.Properties["cursor"], &cursor))
			require.Equal(t, 2048, cursor.MaxLength)
		case scriptFoodChange:
			var action struct {
				Enum []string `json:"enum"`
			}
			require.NoError(t, json.Unmarshal(schema.Properties["name"], &action))
			require.Equal(t, []string{"save_meals", "delete_meals", "toggle_activity"}, action.Enum)
		case scriptFoodPrepare, scriptFoodReviewDecide, scriptFoodReviewProof:
			var kind struct {
				Enum []string `json:"enum"`
			}
			require.NoError(t, json.Unmarshal(schema.Properties["kind"], &kind))
			require.Equal(t, []string{"meals", "activities"}, kind.Enum)
			if entry.Descriptor.Name == scriptFoodReviewDecide {
				var decision struct {
					Enum []string `json:"enum"`
				}
				require.NoError(t, json.Unmarshal(schema.Properties["decision"], &decision))
				require.Equal(t, []string{"accept", "reject"}, decision.Enum)
			}
		case scriptFoodExport:
			require.Len(t, schema.Properties, 1)
			require.Contains(t, schema.Properties, "continuation")
		}
	}
}

func TestFoodCatalogLiveScopeRevocationAndNoEvent(t *testing.T) {
	t.Parallel()
	client := &foodCatalogCapabilities{current: legacyfood.OwnerCapabilities{EventID: "event"}}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: &liveFoodCatalog{food: FoodScriptCatalog{Client: client}}}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	client.current.CanReview, client.current.CanExport = true, true
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Contains(t, string(list), scriptFoodView)
	require.NotContains(t, string(list), scriptFoodExport)
	require.NotContains(t, string(list), scriptFoodReviewRead)
	_, err = host.Registry.Resolve(ctx, "owner", scriptFoodExport)
	require.Error(t, err, "new roles cannot broaden the admitted VM")
	require.Len(t, client.owners, 2)
	fresh, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	freshCtx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(fresh))
	client.current.CanReview, client.current.CanExport = false, false
	_, err = host.discover(freshCtx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"food.review.read"}`),
	})
	require.Error(t, err)
	_, err = host.Registry.Resolve(freshCtx, "owner", scriptFoodExport)
	require.Error(t, err)
	client.current.EventID = ""
	_, err = host.Registry.Resolve(freshCtx, "owner", scriptFoodChange)
	require.Error(t, err, "loss of the active event hides owner tools too")
	client.current = legacyfood.OwnerCapabilities{EventID: "current-event", CanReview: true, CanExport: true}
	help, err := host.discover(freshCtx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"food.export"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "Current export rights are checked before each unsent file")
}

func TestFoodCatalogRoutesOwnerAndAdminCallbacksWithoutGrantingExecution(t *testing.T) {
	t.Parallel()
	client := &foodCatalogCapabilities{current: legacyfood.OwnerCapabilities{
		EventID: "event", CanReview: true, CanExport: true,
	}}
	var calls []string
	denied := &core.ProblemError{Status: 403, Code: "food_review_forbidden"}
	binding := func(family string) ScriptToolEntry {
		return ScriptToolEntry{ResultLimit: 32 << 10,
			Prepare: func(_ context.Context, owner string, update int64, call scriptclient.ToolCall,
				_ agent.Input,
			) (ScriptToolRecord, error) {
				require.Equal(t, "owner", owner)
				require.Equal(t, int64(123), update)
				calls = append(calls, family+":prepare:"+call.Name)
				return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
			},
			Execute: func(_ context.Context, owner string, call scriptclient.ToolCall, record ScriptToolRecord,
				_ *agent.Input,
			) (any, error) {
				require.Equal(t, "owner", owner)
				require.Equal(t, call.Name, record.Outcome.Name)
				calls = append(calls, family+":execute:"+call.Name)
				if call.Name == scriptFoodReviewDecide {
					return nil, denied
				}
				return family, nil
			},
		}
	}
	catalog := FoodScriptCatalog{Client: client, OwnerBinding: binding("owner"), AdminBinding: binding("admin")}
	entries, err := catalog.Entries(t.Context(), "owner")
	require.NoError(t, err)
	for index, entry := range entries {
		family := "owner"
		if index >= 4 {
			family = "admin"
		}
		call := scriptclient.ToolCall{Name: entry.Descriptor.Name}
		record, prepareErr := entry.Prepare(t.Context(), "owner", 123, call, agent.Input{})
		require.NoError(t, prepareErr)
		result, executeErr := entry.Execute(t.Context(), "owner", call, record, &agent.Input{})
		if call.Name == scriptFoodReviewDecide {
			require.ErrorIs(t, executeErr, denied)
			require.Nil(t, result)
		} else {
			require.NoError(t, executeErr)
			require.Equal(t, family, result)
		}
		require.Equal(t, []string{family + ":prepare:" + call.Name, family + ":execute:" + call.Name}, calls)
		calls = nil
	}
	require.Equal(t, []string{"owner"}, client.owners, "binding callbacks do not trigger extra catalog reads")
}

func TestFoodCatalogFailureDoesNotExposePartialCurrentRoles(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled,
		core.DatabaseFailure(&core.ProblemError{Status: 404, Code: "missing"}), errors.New("capability failure"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			client := &foodCatalogCapabilities{current: legacyfood.OwnerCapabilities{
				EventID: "event", CanReview: true, CanExport: true,
			}, failure: failure}
			entries, err := (FoodScriptCatalog{Client: client}).Entries(t.Context(), "owner")
			require.ErrorIs(t, err, failure)
			require.Nil(t, entries)
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}
