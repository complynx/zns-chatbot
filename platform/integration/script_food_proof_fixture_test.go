package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

// foodProofVM lets the real delivery worker run after proof admission and before
// the script continues. The callback still returns its original pending result.
type foodProofVM struct {
	scopeVM

	test    *testing.T
	fixture *fixture
}

func (vm foodProofVM) Execute(ctx context.Context, request scriptclient.Request,
	tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
	return vm.scopeVM.Execute(
		ctx,
		request,
		tools,
		func(ctx context.Context, call scriptclient.ToolCall) (json.RawMessage, error) {
			result, err := callback(ctx, call)
			if err == nil && call.Name == "food.review.proof" {
				pumpBotDeliveries(vm.test, vm.fixture.b)
			}
			return result, err
		},
	)
}

func runFoodProofVM(t *testing.T, f *fixture, update int64, code string) json.RawMessage {
	t.Helper()
	f.b.Scripts = foodProofVM{test: t, fixture: f}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Completed"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, identity.BobTelegramID, "Review food as requested"))
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(
		t.Context(),
		`SELECT content FROM bot.interactions WHERE owner='bob' AND update_id=$1 AND kind='script_runs'`,
		update,
	).Scan(&records))
	require.Len(t, records, 1)
	if records[0].PassRedacted {
		require.Len(t, model.inputs, 1, "retired source must not reach another model turn")
	} else {
		require.Len(t, model.inputs, 2)
		require.Empty(t, records[0].Run.Error)
	}
	return records[0].Run.Result
}
