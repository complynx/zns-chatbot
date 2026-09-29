package integration_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
)

func TestModernOrderReadCompletesAdmittedBinding(t *testing.T) {
	t.Parallel()
	f, order := modernSingleKeyOrder(t, "a")
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{
			Code: fmt.Sprintf(
				`const p=tools.orders.inspect({order_id:%q});return {more:p.more};`,
				order.ID,
			),
			InputJSON: "null",
		}},
		{View: agent.OrdersView, Text: "Read complete"},
	}}
	f.b.Model = model
	handle(t, f.b, message(48900, identity.AliceTelegramID, "Inspect the selected order"))
	var records []agenthost.ScriptRecord
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions
 WHERE owner='alice' AND update_id=48900 AND kind='script_runs'`).Scan(&records))
	require.Len(t, records, 1)
	require.Len(t, records[0].Calls, 1)
	assert.Empty(t, records[0].Calls[0].Outcome.Error, "executed read must complete its admitted call")
	require.Len(t, model.inputs, 2)
	assert.Empty(t, records[0].Run.Error)
	assert.NotEmpty(t, records[0].Calls[0].ModernOrder.ReadSnapshot)
	var page struct {
		More bool   `json:"more"`
		Next string `json:"next_cursor"`
	}
	require.NoError(t, json.Unmarshal(records[0].Calls[0].Outcome.Result, &page))
	assert.True(t, page.More)
	assert.NotEmpty(t, page.Next)
}
