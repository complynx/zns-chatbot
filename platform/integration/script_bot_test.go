package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptRecorder struct {
	requests []scriptclient.Request
	result   json.RawMessage
	cancel   context.CancelFunc
}

func (s *scriptRecorder) Evaluate(_ context.Context, r scriptclient.Request) (json.RawMessage, error) {
	s.requests = append(s.requests, r)
	if s.cancel != nil {
		s.cancel()
		return nil, context.Canceled
	}
	return s.result, nil
}

func TestScriptBotExplicitDataReplansAndReplaysWithoutWorker(t *testing.T) {
	t.Parallel()
	f := setup(t)
	scripts := &scriptRecorder{result: json.RawMessage(`{"sum":5,"actor":"admin"}`)}
	f.b.Scripts = scripts
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View:         "workflow",
			ScriptAction: &agent.ScriptProposal{Code: "return {sum:input.a+input.b};", InputJSON: `{"a":2,"b":3}`},
		},
		{View: "workflow", Text: "The sum is 5"},
	}}
	f.b.Model = model
	update := message(830, identity.AliceTelegramID, "Calculate 2+3")
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.Len(t, scripts.requests, 1)
	assert.JSONEq(t, `{"a":2,"b":3}`, string(scripts.requests[0].Input))
	require.Len(t, model.inputs, 2)
	require.NotNil(t, model.inputs[1].Script)
	require.Len(t, model.inputs[1].Script.Runs, 1)
	assert.Equal(t, 1, model.inputs[1].Script.Remaining)
	assert.JSONEq(t, string(scripts.result), string(model.inputs[1].Script.Runs[0].Result))
	workflow, err := f.b.API.Current(t.Context(), "alice")
	require.NoError(t, err)
	assert.Equal(t, "empty", workflow.State)
	model.plans = []agent.Plan{{View: "workflow", Text: "Hi"}}
	handle(t, f.b, message(831, identity.BobTelegramID, "Hi"))
	assert.Empty(t, model.inputs[len(model.inputs)-1].Script.Runs)
	for _, event := range model.inputs[len(model.inputs)-1].History {
		assert.NotEqual(t, "script_runs", event.Kind)
	}
}

func TestScriptBotBudgetAndDisabledTool(t *testing.T) {
	t.Parallel()
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			scripts := &scriptRecorder{result: json.RawMessage(`1`)}
			if enabled {
				f.b.Scripts = scripts
			}
			model := &knowledgeModel{
				plans: []agent.Plan{
					{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return 1;", InputJSON: "null"}},
				},
			}
			f.b.Model = model
			update := message(840, identity.AliceTelegramID, "Compute")
			handle(t, f.b, update)
			handle(t, f.b, update)
			if enabled {
				assert.Len(t, scripts.requests, agent.MaxScriptRuns)
				assert.Len(t, model.inputs, agent.MaxScriptRuns+1)
			} else {
				assert.Empty(t, scripts.requests)
				assert.Len(t, model.inputs, 1)
			}
		})
	}
}

func TestScriptBotInterruptedReservationSurvivesRetry(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scripts := &scriptRecorder{cancel: cancel, result: json.RawMessage(`2`)}
	f.b.Scripts = scripts
	model := &knowledgeModel{plans: []agent.Plan{
		{View: "workflow", ScriptAction: &agent.ScriptProposal{Code: "return 1;", InputJSON: "null"}},
		{View: "workflow", Text: "The calculation was interrupted"},
	}}
	f.b.Model = model
	update := message(850, identity.AliceTelegramID, "Compute")
	err := f.b.Handle(ctx, update)
	require.ErrorIs(t, err, context.Canceled)
	scripts.cancel = nil
	handle(t, f.b, update)
	require.Len(t, scripts.requests, 1)
	require.Len(t, model.inputs, 2)
	require.Len(t, model.inputs[1].Script.Runs, 1)
	assert.Equal(t, "interrupted", model.inputs[1].Script.Runs[0].Error)
	assert.Equal(t, 1, model.inputs[1].Script.Remaining)
}

func TestScriptBotPreservesSpokenOrderEvidence(t *testing.T) {
	t.Parallel()
	f := setup(t)
	order := intakeOrder(t, f, "first")
	worker := &avWorker{result: avResult(t, false)}
	worker.result.Transcript.Text = "Add shuttle to order " + order.ID
	f.b.AV = worker
	f.b.Scripts = &scriptRecorder{result: json.RawMessage(`{"count":1}`)}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View:         agent.OrdersView,
			ScriptAction: &agent.ScriptProposal{Code: "return {count:input.length};", InputJSON: `["shuttle"]`},
		},
		{
			View:        agent.OrdersView,
			OrderAction: &agent.OrderProposal{Name: "add_extra", OrderID: order.ID, Extra: "shuttle"},
		},
	}}
	f.b.Model = model
	handle(t, f.b, avUpload(t, f, "voice"))
	require.Len(t, model.inputs, 2)
	require.NotNil(t, model.inputs[1].AV)
	assert.Equal(t, worker.result.Transcript.Text, model.inputs[1].AV.Transcript.Text)
	assert.Equal(t, model.inputs[0].AVInspection.Remaining, model.inputs[1].AVInspection.Remaining)
	var choice orders.Choice
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT choice FROM core.orders WHERE id=$1`, order.ID).Scan(&choice))
	assert.Contains(t, choice.Extras, "shuttle")
}
