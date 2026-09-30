package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestAgentDiagnosticPartialScriptAndReplay(t *testing.T) {
	t.Parallel()
	f := setup(t)
	var log bytes.Buffer
	f.b.Logger = observability.NewLogger(&log, observability.LogConfig{Level: slog.LevelDebug})
	executions := 0
	f.b.Scripts = hostScriptFunc(
		func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
			executions++
			scriptCall(ctx, t, callback, "orders.list", "{}")
			scriptCall(ctx, t, callback, "orders.change", `{"name":"create"}`)
			_, err := callback(
				ctx,
				scriptclient.ToolCall{
					Name:      "private-canary-tool",
					Arguments: json.RawMessage(`{"memo":"private-canary"}`),
				},
			)
			require.Error(t, err)
			return nil, errors.New("private-canary-worker-error")
		},
	)
	f.b.Model = &knowledgeModel{plans: []agent.Plan{
		{
			View: agent.OrdersView,
			ScriptAction: &agent.ScriptProposal{
				Code:      `const privateCanary = "private-canary"; return input.map(x => x);`,
				InputJSON: `{"memo":"private-canary"}`,
			},
		},
		{View: agent.OrdersView, Text: "Partial completion"},
	}}
	update := message(21901, identity.AliceTelegramID, "private-canary-user-message")
	handle(t, f.b, update)
	handle(t, f.b, update)
	require.Equal(t, 1, executions)
	require.NotContains(t, log.String(), "private-canary")
	require.NotContains(t, log.String(), "privateCanary")
	var exported bytes.Buffer
	page, err := observability.ExportAgentLog(
		t.Context(),
		bytes.NewReader(log.Bytes()),
		&exported,
		observability.AgentExportOptions{Limit: 100},
	)
	require.NoError(t, err)
	require.Positive(t, page.Records)
	var correlation string
	var attempt uint32
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content->>'correlation',(content->>'attempt')::bigint FROM bot.interactions WHERE owner='alice' AND update_id=21901 AND kind='diagnostic_context'`).
			Scan(&correlation, &attempt),
	)
	require.Equal(t, uint32(2), attempt)
	found := make(map[string]bool)
	for line := range strings.SplitSeq(strings.TrimSpace(exported.String()), "\n") {
		var record observability.AgentRecord
		require.NoError(t, json.Unmarshal([]byte(line), &record))
		require.Equal(t, correlation, record.Correlation)
		found[record.Operation+":"+record.Outcome] = true
		if record.Attempt == 2 && record.Operation == "model.cache" {
			require.True(t, record.Replay)
		}
	}
	for _, key := range []string{"orders.list:no_results", "orders.change:ok", "unknown:denied", "js.run:interrupted", "model.cache:replayed"} {
		require.True(t, found[key], key)
	}
	orders, err := f.b.API.Orders(t.Context(), "alice", "sandbox-festival")
	require.NoError(t, err)
	require.Len(t, orders, 1)
}

func TestAgentDiagnosticFailureClassification(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name    string
		content string
		sql     bool
	}{
		{"SQL", `{"correlation":"private-canary","attempt":"invalid-canary"}`, true},
		{"optional recorder", `{"correlation":"private-canary","attempt":1}`, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			var log bytes.Buffer
			f.b.Logger = observability.NewLogger(&log, observability.LogConfig{})
			_, err := f.db.Exec(
				t.Context(),
				`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',21902,'diagnostic_context',$1)`,
				json.RawMessage(scenario.content),
			)
			require.NoError(t, err)
			err = f.b.Handle(t.Context(), message(21902, identity.AliceTelegramID, "hello"))
			require.NotContains(t, log.String(), "canary")
			if scenario.sql {
				require.ErrorIs(t, err, core.ErrDatabase)
				require.EqualError(t, err, core.ErrDatabase.Error())
				require.Zero(t, f.model.calls)
				return
			}
			require.NoError(t, err)
			require.Contains(t, log.String(), "agent diagnostics omitted")
			require.Equal(t, 1, f.model.calls)
		})
	}
}
