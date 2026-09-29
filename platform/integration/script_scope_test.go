package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
	"github.com/complynx/zns-chatbot/platform/internal/scriptprotocol"
	"github.com/complynx/zns-chatbot/platform/internal/scriptworker"
)

// Only tests run Sobek in process; production keeps its isolated worker.
type scopeVM struct{ before func() }

func (scopeVM) Evaluate(context.Context, scriptclient.Request) (json.RawMessage, error) {
	return nil, errors.New("unexpected legacy evaluation")
}

func (vm scopeVM) Execute(ctx context.Context, request scriptclient.Request,
	tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
	if vm.before != nil {
		vm.before()
	}
	return scriptworker.Execute(ctx, scriptprotocol.ExecuteRequest{
		Code: request.Code, Input: request.Input, Tools: tools,
	}, callback)
}

func TestScriptDiscoveryMatchesRunBindingsAfterGrantAndRevoke(t *testing.T) {
	t.Parallel()
	f := setup(t)
	setBooking := func(allowed bool) {
		_, err := f.db.Exec(t.Context(), `UPDATE core.users SET can_book=$1 WHERE id='alice'`, allowed)
		require.NoError(t, err)
	}
	setBooking(false)
	const code = `const listed = (await tools.$list()).some(t => t.name === "orders.change");
const bound = typeof tools.orders.change === "function";
let help = false, called = false;
if (bound) {
  try { await tools.orders.change.$help(); help = true; } catch (_) {}
  if (!listed) {
    try { await tools.orders.change({name:"create"}); called = true; } catch (_) {}
  }
}
return {listed, bound, help, called};`
	for index, step := range []struct {
		before func()
		want   string
	}{
		{func() { setBooking(true) }, `{"listed":false,"bound":false,"help":false,"called":false}`},
		{nil, `{"listed":true,"bound":true,"help":true,"called":false}`},
		{func() { setBooking(false) }, `{"listed":false,"bound":true,"help":false,"called":false}`},
	} {
		f.b.Scripts = scopeVM{before: step.before}
		model := &knowledgeModel{plans: []agent.Plan{
			{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
			{View: "workflow", Text: "Checked"},
		}}
		f.b.Model = model
		handle(t, f.b, message(int64(9820+index), identity.AliceTelegramID, "Check current tools"))
		require.Len(t, model.inputs, 2)
		runs := model.inputs[1].Script.Runs
		require.NotEmpty(t, runs)
		assert.Empty(t, runs[len(runs)-1].Error)
		assert.JSONEq(t, step.want, string(runs[len(runs)-1].Result))
	}
	var orders int
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.orders WHERE owner='alice'`).Scan(&orders))
	assert.Zero(t, orders, "revoked cached binding must not create an order")
}
