package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type liveOrderCatalog struct {
	liveScriptCatalog

	orders  OrderScriptCatalog
	current core.BusinessCapabilities
	failure error
	owners  []string
}

func (c *liveOrderCatalog) Capabilities(_ context.Context, owner string) (core.BusinessCapabilities, error) {
	c.owners = append(c.owners, owner)
	return c.current, c.failure
}

func (c *liveOrderCatalog) Orders(capability core.BusinessCapabilities) []ScriptToolEntry {
	return c.orders.Entries(capability)
}

func TestOrderCatalogWholeFamilyVisibilityAndOrder(t *testing.T) {
	t.Parallel()
	base := []string{modernOrdersBrowse, modernOrdersEvents, modernOrdersEvent, modernOrdersContacts,
		modernOrdersHistory, modernOrdersHistoryRead, modernOrdersInspect, modernOrdersInstructions, modernOrdersProof}
	admin := []string{modernOrdersInbox, modernOrdersReviewRead, modernOrdersReviewDecide,
		modernOrdersReviewProof, modernOrdersExport}
	for _, test := range []struct {
		name   string
		book   bool
		export bool
	}{
		{name: "owner"}, {name: "book", book: true},
		{name: "review", export: true}, {name: "both", book: true, export: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			expected := append([]string(nil), base...)
			if test.book {
				expected = append(expected, modernOrdersQuote, modernOrdersUpdate)
			}
			if test.export {
				expected = append(expected, admin...)
			}
			if test.book {
				expected = append(expected, modernOrdersChoice)
			}
			entries := (OrderScriptCatalog{
				OrderBinding:  ScriptToolEntry{ResultLimit: 32 << 10},
				ChoiceBinding: ScriptToolEntry{ResultLimit: 32 << 10},
			}).Entries(core.BusinessCapabilities{CanBook: test.book, CanExportOrders: test.export})
			names := make([]string, 0, len(entries))
			for _, entry := range entries {
				names = append(names, entry.Descriptor.Name)
				require.Equal(t, 32<<10, entry.ResultLimit)
			}
			require.Equal(t, expected, names)
		})
	}
}

func TestOrderCatalogRetainsSchemasAndHostBoundIdentity(t *testing.T) {
	t.Parallel()
	entries := (OrderScriptCatalog{}).Entries(core.BusinessCapabilities{CanBook: true, CanExportOrders: true})
	require.Len(t, entries, 17)
	required := map[string][]string{
		modernOrdersInspect: {"order_id"}, modernOrdersHistoryRead: {"entry"},
		modernOrdersInstructions: {"order_id"}, modernOrdersProof: {"order_id"},
		modernOrdersUpdate: {"name"}, modernOrdersReviewRead: {"order_id"},
		modernOrdersReviewDecide: {"order_id", "name"}, modernOrdersReviewProof: {"order_id"},
		modernOrdersChoice: {"operation"},
	}
	for _, entry := range entries {
		var schema struct {
			Type                 string                     `json:"type"`
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             []string                   `json:"required"`
			AdditionalProperties bool                       `json:"additionalProperties"`
		}
		require.NoError(t, json.Unmarshal(entry.Descriptor.InputSchema, &schema))
		require.Equal(t, "object", schema.Type)
		require.False(t, schema.AdditionalProperties)
		require.Equal(t, append([]string{}, required[entry.Descriptor.Name]...), schema.Required)
		for _, private := range []string{"owner", "version", "attempt", "proof", "source", "operation_key"} {
			require.NotContains(t, schema.Properties, private)
		}
		switch entry.Descriptor.Name {
		case modernOrdersInspect, modernOrdersReviewRead:
			require.JSONEq(t, `{"type":"boolean","description":`+
				`"Replay the last successful page after a new turn or restart; do not combine with cursor."}`,
				string(schema.Properties["resume"]))
		case modernOrdersUpdate:
			require.JSONEq(t, `{"enum":["create","edit","delete","cash","cancel_proof","country"]}`,
				string(schema.Properties["name"]))
			require.JSONEq(t, `{"enum":["be","ru"]}`, string(schema.Properties["country"]))
		case modernOrdersReviewDecide:
			require.JSONEq(t, `{"enum":["accept","reject"]}`, string(schema.Properties["name"]))
		case modernOrdersChoice:
			require.JSONEq(t, `{"enum":["begin","patch","read"]}`, string(schema.Properties["operation"]))
			require.JSONEq(t, `{"enum":["summary","choice","catalog"]}`, string(schema.Properties["part"]))
			require.Contains(t, string(schema.Properties["meals"]), `"additionalProperties":false`)
			require.Contains(t, string(schema.Properties["extras"]), `"required":["ref","selected"]`)
			for _, field := range []string{"choice_ref", "event", "order_id", "empty", "cursor", "customer",
				"customer_first_name", "customer_last_name", "customer_patronymus", "days", "meals", "extras"} {
				require.Contains(t, schema.Properties, field)
			}
		case modernOrdersExport:
			require.Empty(t, schema.Properties)
		}
	}
}

func TestOrderCatalogLiveCapabilitiesAndOriginalVmScope(t *testing.T) {
	t.Parallel()
	client := &liveOrderCatalog{current: core.BusinessCapabilities{CanBook: true}}
	host := ScriptHost{Registry: ScriptRegistry{Catalog: client}}
	tools, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	ctx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(tools))
	client.current = core.BusinessCapabilities{CanExportOrders: true}
	list, err := host.discover(ctx, "owner", scriptclient.ToolCall{Name: "$list", Arguments: json.RawMessage(`{}`)})
	require.NoError(t, err)
	require.Contains(t, string(list), modernOrdersInspect)
	require.NotContains(t, string(list), `"name":"`+modernOrdersUpdate+`"`)
	require.NotContains(t, string(list), `"name":"`+modernOrdersChoice+`"`)
	require.NotContains(t, string(list), `"name":"`+modernOrdersReviewDecide+`"`)
	_, err = host.Registry.Resolve(ctx, "owner", modernOrdersUpdate)
	require.Error(t, err, "revoked booking rights deny known-name calls")
	_, err = host.Registry.Resolve(ctx, "owner", modernOrdersReviewDecide)
	require.Error(t, err, "new role cannot broaden this VM")
	require.Len(t, client.owners, 3, "out-of-scope call does not issue another capability query")
	fresh, err := host.Registry.Available(t.Context(), "owner")
	require.NoError(t, err)
	freshCtx := context.WithValue(t.Context(), scriptRunScopeKey{}, scriptRunScope(fresh))
	help, err := host.discover(freshCtx, "owner", scriptclient.ToolCall{
		Name: "$help", Arguments: json.RawMessage(`{"name":"orders.review.decide"}`),
	})
	require.NoError(t, err)
	require.Contains(t, string(help), "explicit accept/reject")
	client.current.CanExportOrders = false
	_, err = host.Registry.Resolve(freshCtx, "owner", modernOrdersReviewDecide)
	require.Error(t, err)
	require.Len(t, client.owners, 6)
}

func TestOrderCatalogDispatchKeepsSeparateBindingsAndDomainDenial(t *testing.T) {
	t.Parallel()
	for _, name := range []string{modernOrdersInspect, modernOrdersUpdate, modernOrdersReviewDecide, modernOrdersChoice} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			denied := &core.ProblemError{Status: 403, Code: "current_event_forbidden"}
			var prepared, executed string
			binding := func(group string) ScriptToolEntry {
				return ScriptToolEntry{ResultLimit: 32 << 10,
					Prepare: func(_ context.Context, owner string, update int64, call scriptclient.ToolCall,
						_ agent.Input,
					) (ScriptToolRecord, error) {
						require.Equal(t, "owner", owner)
						require.Equal(t, int64(123), update)
						prepared = group
						return ScriptToolRecord{Outcome: agent.ScriptToolResult{Name: call.Name}}, nil
					},
					Execute: func(_ context.Context, owner string, call scriptclient.ToolCall, record ScriptToolRecord,
						_ *agent.Input,
					) (any, error) {
						require.Equal(t, "owner", owner)
						require.Equal(t, call.Name, record.Outcome.Name)
						executed = group
						return nil, denied
					},
				}
			}
			client := &liveOrderCatalog{current: core.BusinessCapabilities{CanBook: true, CanExportOrders: true},
				orders: OrderScriptCatalog{OrderBinding: binding("order"), ChoiceBinding: binding("choice")}}
			entry, err := (ScriptRegistry{Catalog: client}).Resolve(t.Context(), "owner", name)
			require.NoError(t, err)
			call := scriptclient.ToolCall{Name: name}
			record, err := entry.Prepare(t.Context(), "owner", 123, call, agent.Input{})
			require.NoError(t, err)
			result, err := entry.Execute(t.Context(), "owner", call, record, &agent.Input{})
			require.ErrorIs(t, err, denied)
			require.Nil(t, result)
			expected := "order"
			if name == modernOrdersChoice {
				expected = "choice"
			}
			require.Equal(t, expected, prepared)
			require.Equal(t, expected, executed)
			require.Equal(t, []string{"owner"}, client.owners)
			require.Equal(t, 32<<10, entry.ResultLimit)
		})
	}
}

func TestOrderCatalogCapabilityFailureHasNoPartialProjection(t *testing.T) {
	t.Parallel()
	for _, failure := range []error{context.Canceled, context.DeadlineExceeded,
		core.DatabaseFailure(&core.ProblemError{Status: 403, Code: "forbidden"}), errors.New("unavailable"),
	} {
		t.Run(failure.Error(), func(t *testing.T) {
			t.Parallel()
			client := &liveOrderCatalog{failure: failure,
				current: core.BusinessCapabilities{CanBook: true, CanExportOrders: true}}
			tools, err := (ScriptRegistry{Catalog: client}).Available(t.Context(), "owner")
			require.Error(t, err)
			require.Nil(t, tools)
			require.Equal(t, []string{"owner"}, client.owners)
		})
	}
}
