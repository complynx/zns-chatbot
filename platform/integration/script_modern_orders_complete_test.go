package integration_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func modernLargeOrder(t *testing.T, multibyte bool) (*fixture, orders.Service, orders.Order) {
	t.Helper()
	f := setup(t)
	s := orders.Service{DB: f.db}
	extras := map[string]orders.Extra{}
	choice := &orders.ChoiceInput{Extras: map[string]json.RawMessage{}}
	stem := strings.Repeat("a", 1000)
	if multibyte {
		stem = strings.Repeat("Ж", 500)
	}
	for index := range 259 {
		key := fmt.Sprintf("%s%03d", stem, index)
		extras[key] = orders.Extra{Price: 100}
		choice.Extras[key] = json.RawMessage(`0`)
	}
	menu, err := orders.Menu()
	require.NoError(t, err)
	canonical, err := orders.Canonicalize(*choice, menu, extras)
	require.NoError(t, err)
	initial, err := json.Marshal(canonical)
	require.NoError(t, err)
	padding := (256 << 10) - len(initial) - len(`"customer":"",`)
	require.Positive(t, padding)
	require.LessOrEqual(t, padding, 1024)
	choice.Customer = strings.Repeat("x", padding)
	_, err = f.db.Exec(t.Context(), `UPDATE core.order_events SET extras=$1 WHERE id='sandbox-festival'`, extras)
	require.NoError(t, err)
	order, err := s.Execute(
		t.Context(),
		"alice",
		orders.Command{
			EventID: "sandbox-festival",
			Name:    "create",
			Origin:  "manual",
			Key:     "large-valid",
			Choice:  choice,
		},
	)
	require.NoError(t, err)
	encoded, err := json.Marshal(order.Choice)
	require.NoError(t, err)
	require.Len(t, encoded, 256<<10)
	return f, s, order
}

func runModernContinuation(t *testing.T, f *fixture, update, user int64, text, code string) json.RawMessage {
	t.Helper()
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.KnowledgeView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.KnowledgeView, Text: "Done"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, user, text))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.Len(t, runs, 1)
	require.Empty(t, runs[0].Error)
	return runs[0].Result
}

func modernReadEvidence(t *testing.T, f *fixture, update int64, assembled []rune) []rune {
	t.Helper()
	var records []struct {
		Calls []struct {
			Outcome agent.ScriptToolResult `json:"outcome"`
		} `json:"calls"`
	}
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&records),
	)
	for _, record := range records {
		require.LessOrEqual(t, len(record.Calls), 8)
		for _, call := range record.Calls {
			if call.Outcome.Name != "orders.inspect" || call.Outcome.Error != "" {
				continue
			}
			var page struct {
				core.ReadChunk

				Offset int `json:"offset"`
			}
			require.NoError(t, json.Unmarshal(call.Outcome.Result, &page))
			part := []rune(page.JSON)
			require.LessOrEqual(t, page.Offset, len(assembled), "no unseen page may be skipped")
			if page.Offset == len(assembled) {
				assembled = append(assembled, part...)
				continue
			}
			require.LessOrEqual(t, page.Offset+len(part), len(assembled))
			assert.Equal(
				t,
				string(assembled[page.Offset:page.Offset+len(part)]),
				page.JSON,
				"resume replays the prior page exactly",
			)
		}
	}
	return assembled
}

func TestModernOrdersCompleteUpperBoundAcrossRestart(t *testing.T) {
	t.Parallel()
	for _, multibyte := range []bool{false, true} {
		t.Run(strconv.FormatBool(multibyte), func(t *testing.T) {
			t.Parallel()
			f, s, order := modernLargeOrder(t, multibyte)
			var assembled []rune
			more := true
			turns := 0
			for more && turns < 20 {
				resume := turns > 0
				update := int64(41000 + turns)
				f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Delivery: f.b.Delivery}
				result := runModernContinuation(
					t,
					f,
					update,
					identity.AliceTelegramID,
					"Read then delete order "+order.ID,
					fmt.Sprintf(`
let page=tools.orders.inspect({order_id:%q,resume:%t});
for(let i=1;i<8 && page.more;i++){page=tools.orders.inspect({order_id:%q,cursor:page.next_cursor});}
return {more:page.more};`, order.ID, resume, order.ID),
				)
				var state struct {
					More bool `json:"more"`
				}
				require.NoError(t, json.Unmarshal(result, &state))
				more = state.More
				assembled = modernReadEvidence(t, f, update, assembled)
				turns++
			}
			require.False(t, more, "bounded calls across durable turns must retrieve the entire valid order")
			require.Greater(t, turns, 2)
			t.Logf("choice_bytes=%d turns=%d read_runes=%d", 256<<10, turns, len(assembled))
			var observed orders.Order
			require.NoError(t, json.Unmarshal([]byte(string(assembled)), &observed))
			assert.Equal(t, order.Choice, observed.Choice)
			f.b = &bot.Bot{DB: f.db, API: f.b.API, Host: f.b.Host, TG: f.b.TG, Delivery: f.b.Delivery}
			result := runModernContinuation(
				t,
				f,
				41099,
				identity.AliceTelegramID,
				"Delete order "+order.ID,
				fmt.Sprintf(`
let unobserved=false;try{tools.orders.update({name:"delete",order_id:%q});unobserved=true;}catch(_){}
const page=tools.orders.inspect({order_id:%q,resume:true});
const deleted=tools.orders.update({name:"delete",order_id:%q});return {unobserved,more:page.more,state:deleted.state};`, order.ID, order.ID, order.ID),
			)
			assert.JSONEq(t, `{"unobserved":false,"more":false,"state":"deleted"}`, string(result))
			_, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
			requireCode(t, err, "order_not_found")
		})
	}
}
