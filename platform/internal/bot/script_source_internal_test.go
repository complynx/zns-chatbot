package bot

import (
	"encoding/json"
	"testing"

	"github.com/complynx/zns-chatbot/platform/internal/agenthost"

	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/observability"

	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/readsource"
)

func TestScriptOrderSourceGeneration(t *testing.T) {
	t.Parallel()
	for _, name := range []string{scriptOrdersChange, modernOrdersUpdate} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			generation := int64(0)
			commandName := actionCreateOrder
			if name == modernOrdersUpdate {
				commandName = modernOrderEdit
			}
			record := agenthost.ScriptToolRecord{
				Order:   &orders.Command{Name: commandName},
				Source:  &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
				Outcome: agent.ScriptToolResult{Name: name},
			}
			agenthost.BindScriptToolKey(&record, "stable-key", 1)
			require.NotNil(t, record.Order.HistoryGeneration)
			require.Zero(t, *record.Order.HistoryGeneration)
			generation = 3
			require.Zero(t, *record.Order.HistoryGeneration, "command owns its admitted snapshot")
		})
	}
}

func TestScriptCannotPublishSourceMetadata(t *testing.T) {
	t.Parallel()
	_, _, err := knowledgeToolProposal(
		scriptclient.ToolCall{
			Name:      "knowledge.suggest",
			Arguments: json.RawMessage(`{"topic":"travel","fact_key":"note","text":"body","published":true}`),
		},
	)
	require.Error(t, err)
	_, _, err = knowledgeToolProposal(
		scriptclient.ToolCall{
			Name: "knowledge.suggest",
			Arguments: json.RawMessage(
				`{"topic":"travel","fact_key":"note","text":"body","source":{"published":true}}`,
			),
		},
	)
	require.Error(t, err)
}

func TestScriptNonContentCommandKeepsHostSourceOnly(t *testing.T) {
	t.Parallel()
	generation := int64(7)
	record := agenthost.ScriptToolRecord{
		Order:  &orders.Command{Name: "country"},
		Source: &readsource.Derivation{Generation: &generation, Authorities: []readsource.Authority{}},
	}
	agenthost.BindScriptToolKey(&record, "stable-country", 1)
	require.Nil(t, record.Order.HistoryGeneration)
	require.Equal(t, int64(7), *record.Source.Generation)
	require.Equal(t, "stable-country", record.Order.Key)
}

func TestScriptCursorStaleOutcome(t *testing.T) {
	t.Parallel()
	_, diagnostic := observability.StartAgentEvent(
		t.Context(),
		observability.AgentEvent{Phase: "tool", Operation: "orders.read"},
	)
	result, outcome, err := agenthost.NormalizeScriptOutcome(
		nil,
		appclient.ErrReadStale,
		diagnostic,
		appclient.ErrReadStale,
		appclient.ErrReadLimit,
	)
	require.NoError(t, err)
	require.Equal(t, "stale", outcome)
	raw, ok := result.(json.RawMessage)
	require.True(t, ok)
	require.JSONEq(t, `{"error":"stale","restart":true}`, string(raw))
	diagnostic.Finish(nil)
}
