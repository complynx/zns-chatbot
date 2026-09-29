package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
	"github.com/complynx/zns-chatbot/platform/internal/sandbox"
)

func TestModernOrdersBoundedRuntime(t *testing.T) {
	t.Parallel()
	for _, scenario := range []struct {
		name      string
		catalog   bool
		multibyte bool
		language  string
	}{
		{"catalog_ascii", true, false, "en"},
		{"catalog_multibyte", true, true, "ru"},
		{"choice_ascii", false, false, "en"},
		{"choice_multibyte", false, true, "ru"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()
			f, service, order := modernLargeOrder(t, scenario.multibyte)
			worker := startModernRuntimeWorker(t)
			if scenario.catalog {
				menu, err := orders.Menu()
				require.NoError(t, err)
				order.Choice, err = orders.Canonicalize(orders.ChoiceInput{}, menu, orders.Extras())
				require.NoError(t, err)
				_, err = f.db.Exec(t.Context(), `UPDATE core.orders SET choice=$1 WHERE id=$2`, order.Choice, order.ID)
				require.NoError(t, err)
			} else {
				// A historical order may retain extras removed from the current catalog.
				_, err := f.db.Exec(t.Context(),
					`UPDATE core.order_events SET extras=$1 WHERE id=$2`, orders.Extras(), order.EventID)
				require.NoError(t, err)
			}
			// Isolate catalog and order-summary budgets from the separate history budget.
			_, err := f.db.Exec(t.Context(), `DELETE FROM core.order_audit WHERE order_id=$1`, order.ID)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, scenario.language)
			require.NoError(t, err)
			f.b.Model = sandbox.FixtureRemote{URL: f.fake.URL + "/lab/model"}
			f.b.Scripts = worker
			f.b.WebAppURL = "https://sandbox.invalid/orders"
			code := fmt.Sprintf(
				`const p=await tools.orders.inspect({order_id:%q});return {more:p.more,offset:p.offset,characters:p.json.length};`,
				order.ID,
			)
			update := queueModernRuntimePlan(t, f, 101, scenario.language, "Inspect order "+order.ID,
				agent.Plan{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
				agent.Plan{View: agent.OrdersView, Text: "Order read checkpoint saved."},
			)
			completeInbox(t, f, update+1)
			assertModernRuntimeFixtureConsumed(t, f, update, 2)
			assertModernRuntimeInspect(t, f, update, !scenario.catalog)
			card := pagingCard(t, f, 101, order.ID)
			assert.Less(t, len(utf16.Encode([]rune(card.Text))), 4096)
			assert.LessOrEqual(t, len(card.Markup.Rows), 12)
			noticeID := i18n.OrderMoreDetails
			if scenario.catalog {
				noticeID = i18n.OrderMoreControls
			}
			notice, err := i18n.Translate(scenario.language, noticeID, nil)
			require.NoError(t, err)
			assert.Contains(t, card.Text, notice)
			require.NotEmpty(t, card.Markup.Rows)
			assert.NotNil(t, card.Markup.Rows[0][0].WebApp)
			_, err = f.db.Exec(t.Context(),
				`UPDATE bot.order_cards SET view_hash='force-refresh' WHERE owner='alice' AND card_key=$1`, order.ID)
			require.NoError(t, err)
			runInboxUntil(t, f, func() bool {
				var hash string
				queryErr := f.db.QueryRow(t.Context(),
					`SELECT view_hash FROM bot.order_cards WHERE owner='alice' AND card_key=$1`, order.ID).Scan(&hash)
				return queryErr == nil && hash != "force-refresh"
			})
			reconciled := pagingCard(t, f, 101, order.ID)
			assert.Equal(t, card.ID, reconciled.ID)
			assert.Equal(t, card.Text, reconciled.Text)
			current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, order.Choice, current.Choice)
			t.Logf("HTTP fixture: 2 turns; external inspect more=%t; inbox drained; card UTF-16 units=%d; reconciled",
				!scenario.catalog, len(utf16.Encode([]rune(card.Text))))
		})
	}
}

func assertModernRuntimeFixtureConsumed(t *testing.T, f *fixture, update int64, steps int) {
	t.Helper()
	address := fmt.Sprintf("%s/lab/model/state?owner=alice&update_id=%d", f.fake.URL, update)
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
	require.NoError(t, err)
	request.Header.Set("X-Sandbox", "1")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusOK, response.StatusCode)
	var state struct {
		Accepted int `json:"accepted"`
		Rejected int `json:"rejected"`
		NextTurn int `json:"next_turn"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&state))
	assert.Equal(t, steps, state.Accepted)
	assert.Equal(t, steps, state.NextTurn)
	assert.Zero(t, state.Rejected)
}

func assertModernRuntimeInspect(t *testing.T, f *fixture, update int64, more bool) {
	t.Helper()
	var runs []struct {
		Run   agent.ScriptRun `json:"run"`
		Calls []struct {
			Outcome agent.ScriptToolResult `json:"outcome"`
		} `json:"calls"`
	}
	require.NoError(t,
		f.db.QueryRow(t.Context(),
			`SELECT content FROM bot.interactions WHERE owner='alice' AND update_id=$1 AND kind='script_runs'`, update).
			Scan(&runs),
	)
	require.Len(t, runs, 1)
	require.Empty(t, runs[0].Run.Error)
	require.Len(t, runs[0].Calls, 1)
	assert.Equal(t, "orders.inspect", runs[0].Calls[0].Outcome.Name)
	assert.Empty(t, runs[0].Calls[0].Outcome.Error)
	var result struct {
		More       bool `json:"more"`
		Offset     int  `json:"offset"`
		Characters int  `json:"characters"`
	}
	require.NoError(t, json.Unmarshal(runs[0].Run.Result, &result))
	assert.Equal(t, more, result.More)
	assert.Zero(t, result.Offset)
	assert.Positive(t, result.Characters)
}
