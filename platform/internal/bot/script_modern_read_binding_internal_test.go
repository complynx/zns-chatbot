package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/agenthost"
	"github.com/complynx/zns-chatbot/platform/internal/appclient"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestModernReadImmutableBinding(t *testing.T) {
	t.Parallel()
	for _, scenario := range []string{"initial", "replay", "continuation", "resume", "changed_snapshot", "cursor_tamper", "target_tamper"} {
		t.Run(scenario, func(t *testing.T) {
			t.Parallel()
			b, order, version := modernReadBindingFixture(t)
			args := modernReadBindingArguments(t, b, order, scenario)
			arguments, err := json.Marshal(args)
			require.NoError(t, err)
			call := scriptclient.ToolCall{Name: modernOrdersInspect, Arguments: arguments}
			prepared, err := b.prepareModernOrderTool(t.Context(), "alice", 48801, call, agent.Input{})
			require.NoError(t, err)
			before, err := json.Marshal(prepared)
			require.NoError(t, err)
			switch scenario {
			case "changed_snapshot":
				version.Store(2)
			case "cursor_tamper":
				args.Cursor = "fabricated"
			case "target_tamper":
				args.OrderID = "another-order"
			}
			call.Arguments, err = json.Marshal(args)
			require.NoError(t, err)
			result, err := b.executeModernOrderTool(t.Context(), "alice", call, prepared, &agent.Input{})
			if scenario == "changed_snapshot" || scenario == "cursor_tamper" || scenario == "target_tamper" {
				require.ErrorIs(t, err, appclient.ErrReadStale)
				assert.Nil(t, result)
			} else {
				require.NoError(t, err)
				page, ok := result.(modernOrderReadChunk)
				require.True(t, ok)
				if scenario == "continuation" {
					assert.Positive(t, page.Offset)
				} else {
					assert.Zero(t, page.Offset)
				}
				if scenario == "replay" {
					replayed, replayErr := b.executeModernOrderTool(
						t.Context(),
						"alice",
						call,
						prepared,
						&agent.Input{},
					)
					require.NoError(t, replayErr)
					assert.Equal(t, result, replayed)
				}
			}
			after, err := json.Marshal(prepared)
			require.NoError(t, err)
			assert.JSONEq(t, string(before), string(after), "execution must not mutate admitted request identity")
		})
	}
}

func modernReadBindingArguments(t *testing.T, b *Bot, order orders.Order, scenario string) modernOrderArguments {
	t.Helper()
	args := modernOrderArguments{Event: order.EventID, OrderID: order.ID}
	if scenario != "continuation" && scenario != "resume" {
		return args
	}
	page, err := core.JSONReadChunk(
		order,
		core.ReadCursor{Actor: "alice", Scope: modernOrdersInspect + ":" + order.EventID + ":" + order.ID},
	)
	require.NoError(t, err)
	require.True(t, page.More)
	snapshot, err := modernOrderFingerprint(order)
	require.NoError(t, err)
	body, err := json.Marshal(page)
	require.NoError(t, err)
	record := agenthost.ScriptRecord{Calls: []agenthost.ScriptToolRecord{
		{
			ModernOrder: &agenthost.ModernOrderRequest{
				Event:        order.EventID,
				ReadOrderID:  order.ID,
				ReadSnapshot: snapshot,
			},
			Outcome: agent.ScriptToolResult{Name: modernOrdersInspect, Result: body},
		},
	}}
	_, err = b.DB.Exec(
		t.Context(),
		`INSERT INTO bot.interactions(owner,update_id,kind,content) VALUES('alice',48800,'script_runs',$1)`,
		[]agenthost.ScriptRecord{record},
	)
	require.NoError(t, err)
	args.Cursor = page.NextCursor
	if scenario == "resume" {
		args.Cursor, args.Resume = "", true
	}
	return args
}

func modernReadBindingFixture(t *testing.T) (*Bot, orders.Order, *atomic.Int64) {
	t.Helper()
	db := foodPendingDatabase(t)
	order := orders.Order{ID: "read-order", EventID: "sandbox-festival", Owner: "alice", Version: 1,
		Choice: orders.Choice{Customer: strings.Repeat("x", 20000)}}
	version := &atomic.Int64{}
	version.Store(order.Version)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/order-events/sandbox-festival/orders/read-order" {
			http.Error(w, "unexpected request", http.StatusNotFound)
			return
		}
		current := order
		current.Version = version.Load()
		assert.NoError(t, json.NewEncoder(w).Encode(current))
	}))
	t.Cleanup(server.Close)
	b := &Bot{
		DB: db,
		API: appclient.Client{
			Base:         server.URL,
			HTTP:         server.Client(),
			SandboxToken: func(string) string { return "synthetic" },
		},
	}
	return b, order, version
}
