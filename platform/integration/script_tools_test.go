package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
)

type scriptLostReplyTransport struct {
	test   *testing.T
	before func()
	lost   bool
}

func (transport *scriptLostReplyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method != http.MethodPost || request.URL.Path != "/v1/order-actions" || transport.lost {
		return http.DefaultTransport.RoundTrip(request)
	}
	transport.before()
	response, err := http.DefaultTransport.RoundTrip(request)
	if err != nil {
		return nil, err
	}
	require.NoError(transport.test, response.Body.Close())
	transport.lost = true
	return nil, errors.New("lost response after commit")
}

type hostScriptFunc func(context.Context, []scriptclient.Tool, scriptclient.Callback) (json.RawMessage, error)

func (f hostScriptFunc) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	return nil, errors.New("legacy evaluator unexpected")
}

func (f hostScriptFunc) Execute(
	ctx context.Context,
	_ scriptclient.Request,
	tools []scriptclient.Tool,
	callback scriptclient.Callback,
) (json.RawMessage, error) {
	return f(ctx, tools, callback)
}

func scriptCall(ctx context.Context, t *testing.T, callback scriptclient.Callback, name, args string) json.RawMessage {
	t.Helper()
	result, err := callback(ctx, scriptclient.ToolCall{Name: name, Arguments: json.RawMessage(args)})
	require.NoError(t, err)
	return result
}

func TestScriptToolsDurableEffectsAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	executions := 0
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			executions++
			require.Len(t, tools, 60)
			require.NoError(t, scriptprotocol.ValidateTools(tools))
			list := scriptCall(ctx, t, callback, "$list", "{}")
			var listed []scriptclient.Tool
			require.NoError(t, json.Unmarshal(list, &listed))
			for _, tool := range listed {
				assert.NotContains(t, tool.Name, "approve")
			}
			help := scriptCall(ctx, t, callback, "$help", `{"name":"orders.change"}`)
			assert.NotContains(t, string(help), "proof_accept")
			_, err := callback(
				ctx,
				scriptclient.ToolCall{Name: "$help", Arguments: json.RawMessage(`{"name":"orders.approve"}`)},
			)
			require.Error(t, err)
			created := scriptCall(ctx, t, callback, "orders.change", `{"name":"create"}`)
			var order agent.OrderSummary
			require.NoError(t, json.Unmarshal(created, &order))
			args, err := json.Marshal(agent.OrderProposal{Name: "add_extra", OrderID: order.ID, Extra: "shuttle"})
			require.NoError(t, err)
			changed := scriptCall(ctx, t, callback, "orders.change", string(args))
			assert.Contains(t, string(changed), "shuttle")
			return json.RawMessage(`{"done":true}`), nil
		},
	)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Done"},
	}}
	f.b.Model = model
	update := message(1850, identity.AliceTelegramID, "Create an order and add shuttle")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Equal(t, 1, executions)
	require.Len(t, model.inputs, 2)
	require.Len(t, model.inputs[1].Script.Runs[0].Calls, 2)
	for _, call := range model.inputs[1].Script.Runs[0].Calls {
		assert.Empty(t, call.Error)
		assert.NotEmpty(t, call.Result)
	}
	list, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Contains(t, list[0].Choice.Extras, "shuttle")
	var admissions int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT jsonb_array_length(content->0->'calls') FROM bot.interactions WHERE owner='alice' AND update_id=1850 AND kind='script_runs'`).
			Scan(&admissions),
	)
	assert.Equal(t, 2, admissions)
}

func TestScriptToolsInterruptedRunRetainsCompletedCalls(t *testing.T) {
	t.Parallel()
	f := setup(t)
	executions := 0
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			executions++
			scriptCall(ctx, t, callback, "orders.change", `{"name":"create"}`)
			return nil, errors.New("worker interrupted")
		},
	)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Order created; script interrupted"},
	}}
	f.b.Model = model
	update := message(1851, identity.AliceTelegramID, "Create an order")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Equal(t, 1, executions)
	run := model.inputs[1].Script.Runs[0]
	assert.Equal(t, "execution_failed", run.Error)
	require.Len(t, run.Calls, 1)
	assert.Empty(t, run.Calls[0].Error)
	assert.NotEmpty(t, run.Calls[0].Result)
	list, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, list, 1)
}

func TestScriptToolsRejectAuthorityInjectionAndForeignOrders(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			for _, args := range []string{`{"name":"create","owner":"bob"}`, `{"name":"create","version":0}`, `{"name":"create","key":"chosen"}`, `{"name":"proof_accept"}`, `{"name":"add_extra","order_id":"foreign","extra":"shuttle"}`} {
				_, err := callback(ctx, scriptclient.ToolCall{Name: "orders.change", Arguments: json.RawMessage(args)})
				require.Error(t, err)
			}
			return json.RawMessage(`null`), nil
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Rejected"},
	}}
	handle(t, f.b, message(1852, identity.AliceTelegramID, "Try invalid changes"))
	list, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	assert.Empty(t, list)
}

func TestScriptToolsPermissionProjectionRefreshesWithinRun(t *testing.T) {
	t.Parallel()
	for _, initiallyAllowed := range []bool{false, true} {
		t.Run(map[bool]string{false: "restricted", true: "revoked"}[initiallyAllowed], func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=$1 WHERE id='alice'`, initiallyAllowed)
			require.NoError(t, err)
			f.b.Scripts = hostScriptFunc(
				func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
					descriptorJSON, marshalErr := json.Marshal(tools)
					require.NoError(t, marshalErr)
					if initiallyAllowed {
						assert.Contains(t, string(descriptorJSON), "orders.change")
						_, updateErr := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
						require.NoError(t, updateErr)
					} else {
						assert.NotContains(t, string(descriptorJSON), "orders.change")
						assert.NotContains(t, string(descriptorJSON), "workflow.select")
					}
					list := scriptCall(ctx, t, callback, "$list", "{}")
					assert.NotContains(t, string(list), "orders.change")
					assert.NotContains(t, string(list), "workflow.select")
					for _, call := range []scriptclient.ToolCall{
						{Name: "$help", Arguments: json.RawMessage(`{"name":"orders.change"}`)},
						{Name: "orders.change", Arguments: json.RawMessage(`{"name":"create"}`)},
						{Name: "workflow.select", Arguments: json.RawMessage(`{"slot_id":"massage-1"}`)},
					} {
						_, callErr := callback(ctx, call)
						require.EqualError(t, callErr, "tool unavailable")
					}
					return json.RawMessage(`null`), nil
				},
			)
			f.b.Model = &knowledgeModel{plans: []agent.Plan{
				{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
				{View: agent.OrdersView, Text: "No permission"},
			}}
			handle(t, f.b, message(1853, identity.AliceTelegramID, "Inspect available tools"))
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&count),
			)
			assert.Zero(t, count)
		})
	}
}

func TestScriptToolsAdmissionPrecedesEffectAndLostReplyIsNotRepeated(t *testing.T) {
	t.Parallel()
	f := setup(t)
	transport := &scriptLostReplyTransport{test: t, before: func() {
		var key string
		require.NoError(
			t,
			f.db.QueryRow(t.Context(), `SELECT content->0->'calls'->0->'order'->>'key' FROM bot.interactions WHERE owner='alice' AND update_id=1854 AND kind='script_runs'`).
				Scan(&key),
		)
		assert.Equal(t, "tg-script-1854-0-0", key)
	}}
	f.b.API.HTTP = &http.Client{Transport: transport}
	executions := 0
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			executions++
			return callback(
				ctx,
				scriptclient.ToolCall{Name: "orders.change", Arguments: json.RawMessage(`{"name":"create"}`)},
			)
		},
	)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Uncertain outcome: inspect orders"},
	}}
	f.b.Model = model
	update := message(1854, identity.AliceTelegramID, "Create order")
	handle(t, f.b, update)
	handle(t, f.b, update)
	assert.Equal(t, 1, executions)
	assert.True(t, transport.lost)
	require.Len(t, model.inputs[1].Script.Runs[0].Calls, 1)
	assert.Equal(t, "interrupted", model.inputs[1].Script.Runs[0].Calls[0].Error)
	var count int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&count))
	assert.Equal(t, 1, count)
}

func TestScriptToolsFollowedByOrdinaryWorkflowActionUsesConfirmedVersion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			return callback(
				ctx,
				scriptclient.ToolCall{Name: "workflow.select", Arguments: json.RawMessage(`{"slot_id":"massage-1"}`)},
			)
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: "workflow", Action: &agent.Proposal{Name: "select", SlotID: "shuttle-1"}},
	}}
	handle(t, f.b, message(1855, identity.AliceTelegramID, "Select massage then select shuttle"))
	current, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "shuttle-1", current.SlotID)
	assert.Equal(t, int64(2), current.Version)
}

func TestScriptToolsCreatedOrderCanBeChangedByOrdinaryProposal(t *testing.T) {
	t.Parallel()
	f := setup(t)
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, OrderAction: &agent.OrderProposal{Name: "add_extra", Extra: "shuttle"}},
	}}
	f.b.Model = model
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			result := scriptCall(ctx, t, callback, "orders.change", `{"name":"create"}`)
			var created agent.OrderSummary
			require.NoError(t, json.Unmarshal(result, &created))
			model.plans[1].OrderAction.OrderID = created.ID
			return result, nil
		},
	)
	handle(t, f.b, message(1856, identity.AliceTelegramID, "Create an order and add shuttle"))
	current, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, current, 1)
	assert.Contains(t, current[0].Choice.Extras, "shuttle")
	assert.Equal(t, int64(2), current[0].Version)
}

func TestScriptToolsAuthorizedReadRefreshesAfterLostWriteReply(t *testing.T) {
	t.Parallel()
	for _, readName := range []string{"orders.list", "orders.get"} {
		t.Run(readName, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			f.b.API.HTTP = &http.Client{Transport: &scriptLostReplyTransport{test: t, before: func() {}}}
			f.b.Scripts = hostScriptFunc(
				func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
					_, err := callback(
						ctx,
						scriptclient.ToolCall{Name: "orders.change", Arguments: json.RawMessage(`{"name":"create"}`)},
					)
					require.Error(t, err)
					var id string
					require.NoError(
						t,
						f.db.QueryRow(t.Context(), `SELECT id FROM core.orders WHERE owner='alice'`).Scan(&id),
					)
					args := "{}"
					if readName == "orders.get" {
						encoded, encodeErr := json.Marshal(map[string]string{"order_id": id})
						require.NoError(t, encodeErr)
						args = string(encoded)
					}
					scriptCall(ctx, t, callback, readName, args)
					change, err := json.Marshal(agent.OrderProposal{Name: "add_extra", OrderID: id, Extra: "shuttle"})
					require.NoError(t, err)
					return callback(ctx, scriptclient.ToolCall{Name: "orders.change", Arguments: change})
				},
			)
			f.b.Model = &knowledgeModel{plans: []agent.Plan{
				{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
				{View: agent.OrdersView, Text: "Done"},
			}}
			handle(t, f.b, message(1857, identity.AliceTelegramID, "Create an order and add shuttle"))
			current, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
			require.NoError(t, err)
			require.Len(t, current, 1)
			assert.Contains(t, current[0].Choice.Extras, "shuttle")
		})
	}
}

func TestScriptToolsWorkflowReadRefreshesOrdinaryProposal(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			_, err := f.b.API.Execute(
				ctx,
				"alice",
				core.Action{
					Name:    "select",
					SlotID:  "massage-1",
					Version: 0,
					Key:     "external-selection",
					Origin:  "manual",
				},
			)
			require.NoError(t, err)
			return callback(ctx, scriptclient.ToolCall{Name: "workflow.get", Arguments: json.RawMessage(`{}`)})
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: "workflow", Action: &agent.Proposal{Name: "select", SlotID: "shuttle-1"}},
	}}
	handle(t, f.b, message(1858, identity.AliceTelegramID, "Read current state and select shuttle"))
	current, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "shuttle-1", current.SlotID)
	assert.Equal(t, int64(2), current.Version)
}

func TestScriptToolsCatalogReadRefreshesResourceGrounding(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			_, err := f.db.Exec(
				ctx,
				`INSERT INTO core.slots(id,title,capacity,price,currency,starts_at) VALUES('new-slot','New slot',10,100,'BYN',now()+interval '1 day')`,
			)
			require.NoError(t, err)
			scriptCall(ctx, t, callback, "workflow.catalog", "{}")
			return callback(
				ctx,
				scriptclient.ToolCall{Name: "workflow.select", Arguments: json.RawMessage(`{"slot_id":"new-slot"}`)},
			)
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: "workflow", Text: "Selected"},
	}}
	handle(t, f.b, message(1859, identity.AliceTelegramID, "Select the new slot when available"))
	current, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "new-slot", current.SlotID)
}

func TestScriptToolsOrderReadDoesNotAuthorizeAmbiguousSelection(t *testing.T) {
	t.Parallel()
	f := setup(t)
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			first := intakeOrder(t, f, "first-read-refresh")
			intakeOrder(t, f, "second-read-refresh")
			scriptCall(ctx, t, callback, "orders.list", "{}")
			args, err := json.Marshal(agent.OrderProposal{Name: "add_extra", OrderID: first.ID, Extra: "shuttle"})
			require.NoError(t, err)
			_, err = callback(ctx, scriptclient.ToolCall{Name: "orders.change", Arguments: args})
			require.Error(t, err)
			return json.RawMessage(`null`), nil
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Select which order"},
	}}
	handle(t, f.b, message(1860, identity.AliceTelegramID, "Add shuttle to my order"))
	current, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, current, 2)
	for _, order := range current {
		assert.NotContains(t, order.Choice.Extras, "shuttle")
	}
}
