package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestModernChoiceFullRuntimeAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, stem := range []string{"a", "Ж", "<&\\\""} {
		t.Run(fmt.Sprintf("%x", stem), func(t *testing.T) {
			t.Parallel()
			worker := startModernRuntimeWorker(t)
			f, original := modernSingleKeyOrder(t, stem)
			f.b.Model = sandbox.FixtureRemote{URL: f.fake.URL + "/lab/model"}
			f.b.Scripts = worker
			f.b.WebAppURL = "https://sandbox.invalid/orders"
			more := true
			for turn := 0; more && turn < 24; turn++ {
				// Reconstruct the host each turn. Only durable receipts can continue reads.
				f.b = &bot.Bot{
					DB:        f.db,
					API:       f.b.API,
					TG:        f.b.TG,
					Model:     f.b.Model,
					Scripts:   worker,
					WebAppURL: f.b.WebAppURL,
				}
				result := modernRuntimeScript(t, f, "Inspect order "+original.ID, fmt.Sprintf(
					`let p=tools.orders.inspect({order_id:%q,resume:%t});for(let i=1;i<8&&p.more;i++)p=tools.orders.inspect({order_id:%q,cursor:p.next_cursor});return {more:p.more};`,
					original.ID,
					turn > 0,
					original.ID,
				))
				var state struct {
					More bool `json:"more"`
				}
				require.NoError(t, json.Unmarshal(result, &state))
				more = state.More
			}
			require.False(t, more)
			result := modernRuntimeScript(t, f, "Change only the name of order "+original.ID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q,resume:true});let d=tools.orders.choice({operation:"begin",order_id:%q});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:"New name"});return {ref:d.choice_ref};`,
				original.ID,
				original.ID,
			))
			var draft struct {
				Ref string `json:"ref"`
			}
			require.NoError(t, json.Unmarshal(result, &draft))
			f.b = &bot.Bot{
				DB:        f.db,
				API:       f.b.API,
				TG:        f.b.TG,
				Model:     f.b.Model,
				Scripts:   worker,
				WebAppURL: f.b.WebAppURL,
			}
			result = modernRuntimeScript(t, f, "Apply the name edit to order "+original.ID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q,resume:true});return tools.orders.update({name:"edit",order_id:%q,choice_ref:%q});`,
				original.ID,
				original.ID,
				draft.Ref,
			))
			t.Logf("name edit through external worker: %s", result)
			service := orders.Service{DB: f.db}
			edited, err := service.Get(t.Context(), "alice", original.EventID, original.ID)
			require.NoError(t, err)
			assert.Equal(t, "New name", edited.Choice.Customer)
			assert.Equal(t, original.Choice.Extras, edited.Choice.Extras)
			restartModernRuntimeStand(t, f)
			result = modernRuntimeScript(t, f, "Create the complete selected order", fmt.Sprintf(
				`let d=tools.orders.choice({operation:"begin",empty:true});const refs=tools.orders.choice({operation:"read",choice_ref:d.choice_ref,part:"catalog"});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:%q,extras:[{ref:0,selected:true}]});return {ref:d.choice_ref,bytes:d.choice_bytes};`,
				original.Choice.Customer,
			))
			var create struct {
				Ref   string `json:"ref"`
				Bytes int    `json:"bytes"`
			}
			require.NoError(t, json.Unmarshal(result, &create))
			assert.Equal(t, 256<<10, create.Bytes)
			quoteCursor := ""
			more = true
			for turn := 0; more && turn < 24; turn++ {
				result = modernRuntimeScript(t, f, "Quote the complete selected choice", fmt.Sprintf(
					`let p=tools.orders.quote({choice_ref:%q,cursor:%q});for(let i=1;i<8&&p.more;i++)p=tools.orders.quote({choice_ref:%q,cursor:p.next_cursor});return {more:p.more,cursor:p.next_cursor||""};`,
					create.Ref,
					quoteCursor,
					create.Ref,
				))
				var page struct {
					More   bool   `json:"more"`
					Cursor string `json:"cursor"`
				}
				require.NoError(t, json.Unmarshal(result, &page))
				more, quoteCursor = page.More, page.Cursor
			}
			require.False(t, more)
			result = modernRuntimeScript(
				t,
				f,
				"Create that exact full order",
				fmt.Sprintf(`return tools.orders.update({name:"create",choice_ref:%q});`, create.Ref),
			)
			var created struct {
				ID string `json:"id"`
			}
			require.NoError(t, json.Unmarshal(result, &created))
			full, err := service.Get(t.Context(), "alice", original.EventID, created.ID)
			require.NoError(t, err)
			assert.Equal(t, original.Choice, full.Choice)
			restartModernRuntimeStand(t, f)
			// A full replacement starts empty and uses compact refs for even a huge key.
			// The updated original requires a new complete observation first.
			more = true
			for turn := 0; more && turn < 24; turn++ {
				result = modernRuntimeScript(t, f, "Inspect order "+original.ID, fmt.Sprintf(
					`let p=tools.orders.inspect({order_id:%q,resume:%t});for(let i=1;i<8&&p.more;i++)p=tools.orders.inspect({order_id:%q,cursor:p.next_cursor});return {more:p.more};`,
					original.ID,
					turn > 0,
					original.ID,
				))
				var state struct {
					More bool `json:"more"`
				}
				require.NoError(t, json.Unmarshal(result, &state))
				more = state.More
			}
			require.False(t, more)
			result = modernRuntimeScript(t, f, "Replace the complete choice of order "+original.ID, fmt.Sprintf(
				`tools.orders.inspect({order_id:%q,resume:true});let d=tools.orders.choice({operation:"begin",order_id:%q,empty:true});d=tools.orders.choice({operation:"patch",choice_ref:d.choice_ref,customer:%q,extras:[{ref:0,selected:true}]});return tools.orders.update({name:"edit",order_id:%q,choice_ref:d.choice_ref});`,
				original.ID,
				original.ID,
				original.Choice.Customer,
				original.ID,
			))
			t.Logf("full replacement through external worker: %s", result)
			restored, err := service.Get(t.Context(), "alice", original.EventID, original.ID)
			require.NoError(t, err)
			assert.Equal(t, original.Choice, restored.Choice)
		})
	}
}

func restartModernRuntimeStand(t *testing.T, f *fixture) {
	t.Helper()
	stand, err := sandbox.New(t.Context(), f.db, "sandbox")
	require.NoError(t, err)
	server := httptest.NewServer(stand.Handler())
	t.Cleanup(server.Close)
	f.fake = server
	f.b.Model = sandbox.FixtureRemote{URL: server.URL + "/lab/model"}
	f.b.TG.Base = server.URL
}

func modernRuntimeScript(t *testing.T, f *fixture, text, code string) json.RawMessage {
	t.Helper()
	update := queueModernRuntimePlan(t, f, 101, "en", text,
		agent.Plan{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		agent.Plan{View: agent.OrdersView, Text: "Order operation recorded."})
	completeInbox(t, f, update+1)
	assertModernRuntimeFixtureConsumed(t, f, update, 2)
	var records []struct {
		Run agent.ScriptRun `json:"run"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	require.Len(t, records, 1)
	require.Empty(t, records[0].Run.Error)
	return records[0].Run.Result
}

func modernSingleKeyOrder(t *testing.T, stem string) (*fixture, orders.Order) {
	t.Helper()
	f := setup(t)
	menu, err := orders.Menu()
	require.NoError(t, err)
	low, high := 1, 256<<10
	for low < high {
		middle := (low + high + 1) / 2
		key := strings.Repeat(stem, middle)
		value, quoteErr := orders.Canonicalize(
			orders.ChoiceInput{Extras: map[string]json.RawMessage{key: json.RawMessage(`0`)}},
			menu,
			map[string]orders.Extra{key: {Price: 100}},
		)
		encoded, _ := json.Marshal(value)
		if quoteErr == nil && len(encoded) <= (256<<10)-512 {
			low = middle
		} else {
			high = middle - 1
		}
	}
	key := strings.Repeat(stem, low)
	extras := map[string]orders.Extra{key: {Price: 100}}
	choice := orders.ChoiceInput{Extras: map[string]json.RawMessage{key: json.RawMessage(`0`)}}
	canonical, err := orders.Canonicalize(choice, menu, extras)
	require.NoError(t, err)
	encoded, err := json.Marshal(canonical)
	require.NoError(t, err)
	choice.Customer = strings.Repeat("x", (256<<10)-len(encoded)-len(`"customer":"",`))
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET extras=$1 WHERE id='sandbox-festival'`, extras)
	require.NoError(t, err)
	order, err := f.b.API.ExecuteOrder(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "boundary-create",
			Choice:  &choice,
		},
	)
	require.NoError(t, err)
	encoded, err = json.Marshal(order.Choice)
	require.NoError(t, err)
	require.Len(t, encoded, 256<<10)
	return f, order
}
