package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/conversation"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type scriptPageRead struct {
	Events     []conversation.Event `json:"events"`
	Orders     []agent.OrderSummary `json:"orders"`
	NextCursor string               `json:"next_cursor"`
	More       bool                 `json:"more"`
}

type scriptFullOrderRead struct {
	JSON       string `json:"json"`
	NextCursor string `json:"next_cursor"`
	More       bool   `json:"more"`
}

func runScriptReads(t *testing.T, f *fixture, execution hostScriptFunc) *knowledgeModel {
	t.Helper()
	f.b.Scripts = execution
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Read completed"},
	}}
	f.b.Model = model
	handle(t, f.b, message(1989, identity.AliceTelegramID, "Read my saved history and orders"))
	return model
}

func TestScriptReadPagesContinueWithoutSkipping(t *testing.T) {
	t.Parallel()
	f := setup(t)
	choice := orders.Choice{Extras: map[string]orders.Money{}, Days: map[string]orders.Day{}}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.orders(id,event_id,owner,version,choice,state,created_at) SELECT 'paged-'||i,'sandbox-festival','alice',1,$1,'unpaid','2026-01-01'::timestamptz+i*interval '1 second' FROM generate_series(1,26) i`,
		choice,
	)
	require.NoError(t, err)
	service := conversation.Service{DB: f.db}
	require.NoError(t, service.Append(t.Context(), "alice", "older", "user", strings.Repeat("<", 5000)))
	require.NoError(t, service.Append(t.Context(), "alice", "newer", "user", strings.Repeat(">", 5000)))
	require.NoError(t, service.Append(t.Context(), "bob", "private", "user", "foreign private marker"))
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, tools []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				require.Len(t, tools, 60)
				for _, tool := range tools {
					assert.Empty(t, tool.InputSchema)
					assert.Empty(t, tool.Description)
				}
				for _, name := range []string{"history.page", "orders.page"} {
					first := scriptCall(ctx, t, callback, name, "{}")
					require.LessOrEqual(t, len(first), 32<<10)
					assert.NotContains(t, string(first), "foreign private marker")
					var page scriptPageRead
					require.NoError(t, json.Unmarshal(first, &page))
					require.True(t, page.More)
					args, marshalErr := json.Marshal(map[string]string{"cursor": page.NextCursor})
					require.NoError(t, marshalErr)
					second := scriptCall(ctx, t, callback, name, string(args))
					require.LessOrEqual(t, len(second), 32<<10)
					var next scriptPageRead
					require.NoError(t, json.Unmarshal(second, &next))
					if name == "history.page" {
						require.Len(t, page.Events, 1)
						require.Len(t, next.Events, 1)
						assert.NotEqual(t, page.Events[0].ID, next.Events[0].ID)
						assert.Equal(t, strings.Repeat(">", 5000), page.Events[0].Text)
						assert.Equal(t, strings.Repeat("<", 5000), next.Events[0].Text)
					} else {
						require.Len(t, page.Orders, 25)
						require.Len(t, next.Orders, 1)
						assert.Equal(t, "paged-26", next.Orders[0].ID)
						assert.False(t, next.More)
					}
				}
				return json.RawMessage(`{"read":true}`), nil
			},
		),
	)
}

func TestScriptFullOrderChunksAndStaleVersion(t *testing.T) {
	t.Parallel()
	f := setup(t)
	text := strings.Repeat("<", 1020) + "🌍"
	choice := orders.Choice{
		Customer:   text,
		FirstName:  text,
		LastName:   text,
		Patronymic: text,
		Extras:     map[string]orders.Money{},
		Days:       map[string]orders.Day{},
	}
	_, err := f.db.Exec(
		t.Context(),
		`INSERT INTO core.orders(id,event_id,owner,version,choice,state) VALUES('chunked','sandbox-festival','alice',1,$1,'unpaid'),('foreign','sandbox-festival','bob',1,$1,'unpaid')`,
		choice,
	)
	require.NoError(t, err)
	model := runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				var complete strings.Builder
				cursor := ""
				firstCursor := ""
				for {
					args, marshalErr := json.Marshal(map[string]string{"order_id": "chunked", "cursor": cursor})
					require.NoError(t, marshalErr)
					data := scriptCall(ctx, t, callback, "orders.read", string(args))
					require.LessOrEqual(t, len(data), 32<<10)
					var chunk scriptFullOrderRead
					require.NoError(t, json.Unmarshal(data, &chunk))
					complete.WriteString(chunk.JSON)
					if !chunk.More {
						break
					}
					cursor = chunk.NextCursor
					if firstCursor == "" {
						firstCursor = cursor
					}
				}
				var actual orders.Order
				require.NoError(t, json.Unmarshal([]byte(complete.String()), &actual))
				assert.Equal(t, choice, actual.Choice)
				_, updateErr := f.db.Exec(ctx, `UPDATE core.orders SET version=version+1 WHERE id='chunked'`)
				require.NoError(t, updateErr)
				stale, _ := json.Marshal(map[string]string{"order_id": "chunked", "cursor": firstCursor})
				var log bytes.Buffer
				recorder, recordErr := observability.NewAgentEvents(
					slog.New(slog.NewJSONHandler(&log, nil)),
					"00000000-0000-4000-8000-000000000001",
					1,
				)
				require.NoError(t, recordErr)
				staleResult, callErr := callback(
					observability.WithAgentEvents(ctx, recorder),
					scriptclient.ToolCall{Name: "orders.read", Arguments: stale},
				)
				require.NoError(t, callErr)
				require.JSONEq(t, `{"error":"stale","restart":true}`, string(staleResult))
				lines := strings.Split(strings.TrimSpace(log.String()), "\n")
				var envelope struct {
					Event string `json:"agent_event"`
				}
				require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &envelope))
				var diagnostic observability.AgentRecord
				require.NoError(t, json.Unmarshal([]byte(envelope.Event), &diagnostic))
				assert.Equal(t, "error", diagnostic.Outcome)
				assert.Equal(t, "conflict", diagnostic.ErrorCode)
				assert.NotContains(t, log.String(), "stale order read")
				_, callErr = callback(
					ctx,
					scriptclient.ToolCall{Name: "orders.read", Arguments: json.RawMessage(`{"order_id":"foreign"}`)},
				)
				require.Error(t, callErr)
				resumed := scriptCall(ctx, t, callback, "orders.read", `{"order_id":"chunked"}`)
				var snapshot struct {
					Version int64 `json:"version"`
				}
				require.NoError(t, json.Unmarshal(resumed, &snapshot))
				assert.Equal(t, int64(2), snapshot.Version)
				return json.RawMessage(`{"read":true}`), nil
			},
		),
	)
	require.Len(t, model.inputs, 2)
	found := false
	for _, call := range model.inputs[1].Script.Runs[0].Calls {
		if call.Error == "stale" {
			found = true
			require.JSONEq(t, `{"error":"stale","restart":true}`, string(call.Result))
		}
	}
	require.True(t, found, "next model must receive a recoverable stale outcome")
}
