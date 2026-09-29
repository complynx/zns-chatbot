package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/agent"
	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func foodSubmittedFixture(t *testing.T) (*fixture, legacyfood.Service, legacyfood.Order) {
	t.Helper()
	f, s := foodBotFixture(t)
	order, err := s.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{EventID: "food-bot", Name: "toggle_activity", Activity: "yoga", Key: "review-fixture"},
	)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.order_proofs(id,owner,filename,body) VALUES('review-proof','alice','receipt.pdf',$1)`,
		[]byte("%PDF-1.4 PRIVATE FOOD PROOF"),
	)
	require.NoError(t, err)
	for _, kind := range []string{legacyfood.Meals, legacyfood.Activity} {
		_, err = f.db.Exec(
			t.Context(),
			`INSERT INTO core.food_payments(order_id,kind,generation,status,proof_id,received_at) VALUES($1,$2,1,'proof_submitted','review-proof',now())`,
			order.ID,
			kind,
		)
		require.NoError(t, err)
	}
	order, err = s.Get(t.Context(), "alice", "food-bot", order.ID)
	require.NoError(t, err)
	return f, s, order
}

func runFoodAdminVM(t *testing.T, f *fixture, update, telegramID int64, code string) json.RawMessage {
	t.Helper()
	f.b.Scripts = scopeVM{}
	model := &knowledgeModel{plans: []agent.Plan{
		{View: agent.OrdersView, ScriptAction: &agent.ScriptProposal{Code: code, InputJSON: "null"}},
		{View: agent.OrdersView, Text: "Completed"},
	}}
	f.b.Model = model
	handle(t, f.b, message(update, telegramID, "Review food as requested"))
	require.Len(t, model.inputs, 2)
	runs := model.inputs[1].Script.Runs
	require.NotEmpty(t, runs)
	require.Empty(t, runs[len(runs)-1].Error)
	return runs[len(runs)-1].Result
}

func TestScriptFoodReviewerSobekIndependentKinds(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, s, order := foodSubmittedFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='bob'`, language)
			require.NoError(t, err)
			result := runFoodAdminVM(t, f, 28001, identity.BobTelegramID, `const queue=tools.food.review.queue({});
const read=tools.food.review.read({order_id:queue.items[0].order_id});
const proof=tools.food.review.proof({kind:"meals"});
const meals=tools.food.review.decide({kind:"meals",decision:"accept"});
const activities=tools.food.review.decide({kind:"activities",decision:"reject"});
return {queue,read,proof,meals,activities};`)
			assert.NotContains(t, string(result), "review-proof")
			assert.NotContains(t, string(result), "PRIVATE FOOD PROOF")
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(result, &fields))
			assert.JSONEq(t, `{"displayed":true}`, string(fields["proof"]))
			got, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, legacyfood.Paid, got.MealPayment.Status)
			assert.Equal(t, legacyfood.Rejected, got.ActivityPayment.Status)
			assert.EqualValues(t, 1, got.MealPayment.Generation)
			assert.EqualValues(t, 1, got.ActivityPayment.Generation)
			assert.Equal(t, "bob", got.MealPayment.ConfirmedBy)
			handle(t, f.b, message(28001, identity.BobTelegramID, "Review food as requested"))
			replay, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, got.Version, replay.Version)
		})
	}
}

func TestScriptFoodReviewerHiddenAndRevoked(t *testing.T) {
	t.Parallel()
	f, _, _ := foodSubmittedFixture(t)
	const discover = `return {names:tools.$list({}).map(t=>t.name),review:typeof tools.food.review,export:typeof tools.food.export};`
	result := runFoodAdminVM(t, f, 28002, identity.AliceTelegramID, discover)
	assert.NotContains(t, string(result), "food.review")
	assert.NotContains(t, string(result), "food.export")
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(result, &fields))
	assert.Equal(t, `"undefined"`, string(fields["review"]))
	f.b.Scripts = scopeVM{before: func() {
		_, err := f.db.Exec(
			t.Context(),
			`UPDATE core.food_admins SET can_review=false,can_export=false WHERE owner='bob'`,
		)
		require.NoError(t, err)
	}}
	model := &knowledgeModel{plans: []agent.Plan{
		{
			View: agent.OrdersView,
			ScriptAction: &agent.ScriptProposal{
				Code:      `let help=false,call=false; try {tools.food.review.read.$help();help=true;}catch(_){} try{tools.food.review.queue({});call=true;}catch(_){} return {help,call,names:tools.$list({}).map(t=>t.name)};`,
				InputJSON: "null",
			},
		},
		{View: agent.OrdersView, Text: "Checked"},
	}}
	f.b.Model = model
	handle(t, f.b, message(28003, identity.BobTelegramID, "Check review"))
	require.Len(t, model.inputs, 2)
	result = model.inputs[1].Script.Runs[0].Result
	require.NoError(t, json.Unmarshal(result, &fields))
	assert.Equal(t, "false", string(fields["help"]))
	assert.Equal(t, "false", string(fields["call"]))
	assert.NotContains(t, string(result), "food.review")
}

func TestScriptFoodReviewerStaleAndAuthorityFields(t *testing.T) {
	t.Parallel()
	for _, change := range []string{"replacement", "revoke", "event", "restricted"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			f, s, order := foodSubmittedFixture(t)
			f.b.Scripts = hostScriptFunc(
				func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
					read := scriptCall(ctx, t, callback, "food.review.read", fmt.Sprintf(`{"order_id":%q}`, order.ID))
					var chunk core.ReadChunk
					require.NoError(t, json.Unmarshal(read, &chunk))
					assert.NotContains(t, chunk.JSON, "review-proof")
					_, err := callback(
						ctx,
						scriptclient.ToolCall{
							Name:      "food.review.decide",
							Arguments: json.RawMessage(`{"kind":"meals","decision":"accept","version":1}`),
						},
					)
					require.Error(t, err)
					switch change {
					case "replacement":
						_, err = f.db.Exec(
							ctx,
							`INSERT INTO core.food_payments(order_id,kind,generation,status,proof_id,received_at) VALUES($1,'meals',2,'proof_submitted','review-proof',now());`,
							order.ID,
						)
						require.NoError(t, err)
						_, err = f.db.Exec(ctx, `UPDATE core.food_orders SET version=version+1 WHERE id=$1`, order.ID)
					case "revoke":
						_, err = f.db.Exec(ctx, `UPDATE core.food_admins SET can_review=false WHERE owner='bob'`)
					case "restricted":
						_, err = f.db.Exec(ctx, `UPDATE core.users SET can_book=false WHERE id='bob'`)
					case "event":
						_, err = f.db.Exec(
							ctx,
							`UPDATE core.pass_events SET finishes_at='2000-01-01Z' WHERE id='food-bot'`,
						)
					}
					require.NoError(t, err)
					_, err = callback(
						ctx,
						scriptclient.ToolCall{
							Name:      "food.review.decide",
							Arguments: json.RawMessage(`{"kind":"meals","decision":"accept"}`),
						},
					)
					require.Error(t, err)
					return json.RawMessage(`{"denied":true}`), nil
				},
			)
			f.b.Model = &knowledgeModel{
				plans: []agent.Plan{
					{
						View:         agent.OrdersView,
						ScriptAction: &agent.ScriptProposal{Code: "return null;", InputJSON: "null"},
					},
					{View: agent.OrdersView, Text: "Checked"},
				},
			}
			handle(t, f.b, message(28004, identity.BobTelegramID, "Accept meal payment"))
			got, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			assert.Equal(t, legacyfood.Submitted, got.MealPayment.Status)
			assert.Empty(t, got.MealPayment.ConfirmedBy)
		})
	}
}

func TestScriptFoodExportContinuationReplayAndRevoke(t *testing.T) {
	t.Parallel()
	f, _, _ := foodSubmittedFixture(t)
	transport := &foodExportSecondFailure{}
	f.b.TG.HTTP = &http.Client{Transport: transport}
	first := runFoodAdminVM(t, f, 28010, identity.BobTelegramID, `return tools.food.export({});`)
	var result struct {
		Continuation string `json:"continuation"`
		Complete     bool   `json:"complete"`
	}
	require.NoError(t, json.Unmarshal(first, &result))
	require.NotEmpty(t, result.Continuation)
	assert.False(t, result.Complete)
	assert.EqualValues(t, 2, transport.documents.Load())
	assert.NotContains(t, string(first), "alice")
	assert.NotContains(t, string(first), "PRIVATE")
	// A new Bot value has no in-memory continuation state.
	restarted := *f.b
	f.b = &restarted
	code := fmt.Sprintf(`return tools.food.export({continuation:%q});`, result.Continuation)
	_, err := f.db.Exec(t.Context(), `UPDATE core.food_admins SET can_export=false WHERE owner='bob'`)
	require.NoError(t, err)
	denied := runFoodAdminVM(t, f, 28011, identity.BobTelegramID, `return {available:typeof tools.food.export};`)
	assert.JSONEq(t, `{"available":"undefined"}`, string(denied))
	assert.EqualValues(t, 2, transport.documents.Load())
	_, err = f.db.Exec(t.Context(), `UPDATE core.food_admins SET can_export=true WHERE owner='bob'`)
	require.NoError(t, err)
	for _, update := range []int64{28012, 28013} {
		done := runFoodAdminVM(t, f, update, identity.BobTelegramID, code)
		require.NoError(t, json.Unmarshal(done, &result))
		assert.True(t, result.Complete)
		assert.EqualValues(t, 3, transport.documents.Load())
	}
	var receipts int
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions WHERE owner='bob' AND update_id=28010 AND kind IN ('food_orders_export','food_summary_export')`).
			Scan(&receipts),
	)
	assert.Equal(t, 2, receipts)
	// Even another authorized exporter cannot use the owner's continuation.
	_, err = f.db.Exec(
		t.Context(),
		`INSERT INTO core.food_admins(event_id,owner,can_export) VALUES('food-bot','alice',true)`,
	)
	require.NoError(t, err)
	stolen := runFoodAdminVM(
		t,
		f,
		28014,
		identity.AliceTelegramID,
		fmt.Sprintf(
			`let denied=false;try { tools.food.export({continuation:%q}); } catch (_) {denied=true;}return {denied};`,
			result.Continuation,
		),
	)
	assert.JSONEq(t, `{"denied":true}`, string(stolen))
	assert.EqualValues(t, 3, transport.documents.Load())
}
