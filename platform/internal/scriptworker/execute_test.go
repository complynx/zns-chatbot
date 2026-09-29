package scriptworker_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

func TestExecuteDiscoveryAndAwait(t *testing.T) {
	t.Parallel()
	calls := 0
	request := scriptprotocol.ExecuteRequest{
		Code:  `const listed=tools.$list(); const help=tools.orders.get.$help();const result=await tools.orders.get({id:input.id});return {listed,help,result};`,
		Input: json.RawMessage(`{"id":7}`),
		Tools: []scriptprotocol.Tool{
			{
				Name:         "orders.get",
				Description:  "Read one order",
				InputSchema:  json.RawMessage(`{"type":"object"}`),
				ResultSchema: json.RawMessage(`{"type":"object"}`),
			},
		},
	}
	result, err := scriptworker.Execute(
		t.Context(),
		request,
		func(_ context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
			if call.Name == "$list" {
				return json.RawMessage(`[{"name":"orders.get","description":"Read one order"}]`), nil
			}
			if call.Name == "$help" {
				return json.Marshal(request.Tools[0])
			}
			calls++
			require.Equal(t, "orders.get", call.Name)
			require.JSONEq(t, `{"id":7}`, string(call.Arguments))
			return json.RawMessage(`{"price":12}`), nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	require.JSONEq(
		t,
		`{"listed":[{"name":"orders.get","description":"Read one order"}],"help":{"name":"orders.get","description":"Read one order","input_schema":{"type":"object"},"result_schema":{"type":"object"}},"result":{"price":12}}`,
		string(result),
	)
}

func TestExecuteBudgetsAndDataBoundary(t *testing.T) {
	t.Parallel()
	for _, code := range []string{`while(true){}`, `return new Promise(()=>{});`, `return {get value(){return 1;}};`, `return tools.missing({});`, `for(let i=0;i<9;i++)tools.read({});return 1;`, `return tools.read({get id(){return 1;}});`} {
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			_, err := scriptworker.Execute(
				t.Context(),
				scriptprotocol.ExecuteRequest{
					Code:  code,
					Input: json.RawMessage(`null`),
					Tools: []scriptprotocol.Tool{{Name: "read"}},
				},
				func(context.Context, scriptprotocol.ToolCall) (json.RawMessage, error) {
					return json.RawMessage(`{}`), nil
				},
			)
			require.Error(t, err)
		})
	}
}

func TestExecuteHostWaitDoesNotUseComputeBudget(t *testing.T) {
	t.Parallel()
	result, err := scriptworker.Execute(
		t.Context(),
		scriptprotocol.ExecuteRequest{
			Code:  `return await tools.read({});`,
			Input: json.RawMessage(`null`),
			Tools: []scriptprotocol.Tool{{Name: "read"}},
		},
		func(ctx context.Context, _ scriptprotocol.ToolCall) (json.RawMessage, error) {
			timer := time.NewTimer(300 * time.Millisecond)
			defer timer.Stop()
			select {
			case <-timer.C:
				return json.RawMessage(`42`), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	)
	require.NoError(t, err)
	require.JSONEq(t, `42`, string(result))
}

func TestExecuteCatalogIsActorScoped(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"orders.get", "payments.approve"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := scriptworker.Execute(
				t.Context(),
				scriptprotocol.ExecuteRequest{
					Code:  `return {orders:typeof tools.orders,payments:typeof tools.payments};`,
					Input: json.RawMessage(`null`),
					Tools: []scriptprotocol.Tool{{Name: name}},
				},
				func(context.Context, scriptprotocol.ToolCall) (json.RawMessage, error) {
					t.Fatal("unexpected business call")
					return nil, nil
				},
			)
			require.NoError(t, err)
			if name == "orders.get" {
				require.JSONEq(t, `{"orders":"object","payments":"undefined"}`, string(result))
			} else {
				require.JSONEq(t, `{"orders":"undefined","payments":"object"}`, string(result))
			}
		})
	}
}

func TestExecuteDiscoveryRechecksHost(t *testing.T) {
	t.Parallel()
	discoveries := 0
	result, err := scriptworker.Execute(
		t.Context(),
		scriptprotocol.ExecuteRequest{
			Code:  `const before=tools.$list();const after=tools.$list();let denied=false;try{tools.orders.get.$help()}catch(e){denied=true}return {before,after,denied};`,
			Input: json.RawMessage(`null`),
			Tools: []scriptprotocol.Tool{{Name: "orders.get"}},
		},
		func(_ context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
			discoveries++
			if call.Name == "$help" {
				return nil, context.Canceled
			}
			if discoveries == 1 {
				return json.RawMessage(`[{"name":"orders.get"}]`), nil
			}
			return json.RawMessage(`[]`), nil
		},
	)
	require.NoError(t, err)
	require.Equal(t, 3, discoveries)
	require.JSONEq(t, `{"before":[{"name":"orders.get"}],"after":[],"denied":true}`, string(result))
}

func TestExecuteRejectsUnhandledAsyncFailures(t *testing.T) {
	t.Parallel()
	for name, code := range map[string]string{
		"detached async": `async function task(){tools.write({});} task(); return {ok:true};`,
		"detached then":  `Promise.resolve().then(()=>tools.write({})); return {ok:true};`,
		"serializer job": `return new Proxy({ok:true},{ownKeys(target){Promise.resolve().then(()=>tools.write({}));return Reflect.ownKeys(target)}});`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := scriptworker.Execute(
				t.Context(),
				scriptprotocol.ExecuteRequest{
					Code:  code,
					Input: json.RawMessage(`null`),
					Tools: []scriptprotocol.Tool{{Name: "write"}},
				},
				func(context.Context, scriptprotocol.ToolCall) (json.RawMessage, error) { return nil, context.Canceled },
			)
			require.ErrorIs(t, err, scriptworker.ErrExecution)
			require.Empty(t, result)
		})
	}
}

func TestExecuteAllowsHandledAsyncFailures(t *testing.T) {
	t.Parallel()
	for name, code := range map[string]string{
		"direct catch":  `try{tools.write({})}catch(e){}return {ok:true};`,
		"promise catch": `Promise.resolve().then(()=>tools.write({})).catch(()=>{});return {ok:true};`,
		"async catch":   `async function task(){tools.write({});}const p=task();p.catch(()=>{});return {ok:true};`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			result, err := scriptworker.Execute(
				t.Context(),
				scriptprotocol.ExecuteRequest{
					Code:  code,
					Input: json.RawMessage(`null`),
					Tools: []scriptprotocol.Tool{{Name: "write"}},
				},
				func(context.Context, scriptprotocol.ToolCall) (json.RawMessage, error) { return nil, context.Canceled },
			)
			require.NoError(t, err)
			require.JSONEq(t, `{"ok":true}`, string(result))
		})
	}
}

func TestExecuteBoundedLargeNameOnlyCatalog(t *testing.T) {
	t.Parallel()
	descriptors := make([]scriptprotocol.Tool, 0, scriptprotocol.MaxTools)
	for index := range scriptprotocol.MaxTools {
		descriptors = append(descriptors, scriptprotocol.Tool{Name: fmt.Sprintf("catalog.tool%d", index)})
	}
	code := fmt.Sprintf(
		`const help=tools.catalog.tool%d.$help();const value=await tools.catalog.tool%d({});return {help,value,hidden:typeof tools.hidden};`,
		scriptprotocol.MaxTools-1,
		scriptprotocol.MaxTools-1,
	)
	result, err := scriptworker.Execute(
		t.Context(),
		scriptprotocol.ExecuteRequest{Code: code, Input: json.RawMessage(`null`), Tools: descriptors},
		func(_ context.Context, call scriptprotocol.ToolCall) (json.RawMessage, error) {
			if call.Name == "$help" {
				return json.RawMessage(`{"description":"Current host help","input_schema":{"type":"object"}}`), nil
			}
			require.Equal(t, fmt.Sprintf("catalog.tool%d", scriptprotocol.MaxTools-1), call.Name)
			return json.RawMessage(`{"owner_scoped":true}`), nil
		},
	)
	require.NoError(t, err)
	require.JSONEq(
		t,
		`{"help":{"description":"Current host help","input_schema":{"type":"object"}},"value":{"owner_scoped":true},"hidden":"undefined"}`,
		string(result),
	)
}
