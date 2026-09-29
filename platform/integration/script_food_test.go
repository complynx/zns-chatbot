package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/core"
	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

const foodToolMeals = `{"friday":{"lunch":{"type":"individual-items","items":[0]}}}`

func scriptFoodRead(ctx context.Context, t *testing.T, callback scriptclient.Callback) legacyfood.View {
	t.Helper()
	raw := scriptCall(ctx, t, callback, "food.view", `{}`)
	var chunk core.ReadChunk
	require.NoError(t, json.Unmarshal(raw, &chunk))
	require.False(t, chunk.More)
	var view legacyfood.View
	require.NoError(t, json.Unmarshal([]byte(chunk.JSON), &view))
	return view
}

func TestScriptFoodOwnerChangesQuotePaymentReplay(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, service := foodBotFixture(t)
			_, err := f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						list := scriptCall(ctx, t, callback, "$list", `{}`)
						assert.Contains(t, string(list), "food.payment.prepare")
						assert.NotContains(t, string(list), "food.review")
						view := scriptFoodRead(ctx, t, callback)
						assert.Equal(t, "food-bot", view.Event.ID)
						assert.Equal(t, "alice", view.Order.Owner)
						assert.Contains(t, string(view.Event.Menu), "Суп")
						quote := scriptCall(ctx, t, callback, "food.quote", `{"meals":`+foodToolMeals+`}`)
						assert.JSONEq(t, `{"total":185,"complete":true}`, string(quote))
						scriptCall(ctx, t, callback, "food.change", `{"name":"save_meals","meals":`+foodToolMeals+`}`)
						scriptCall(ctx, t, callback, "food.change", `{"name":"toggle_activity","activity":"yoga"}`)
						scriptCall(ctx, t, callback, "food.payment.prepare", `{"kind":"meals"}`)
						return json.RawMessage(`{"prepared":"meals"}`), nil
					},
				),
			)
			handle(t, f.b, message(1989, identity.AliceTelegramID, "Read my saved history and orders"))
			view, err := service.View(t.Context(), "alice", "food-bot", "")
			require.NoError(t, err)
			assert.EqualValues(t, 3, view.Order.Version)
			assert.True(t, view.Order.Activities["yoga"])
			assert.EqualValues(t, 18500, view.Order.MealTotal)
			assert.Equal(t, legacyfood.Pending, view.Order.MealPayment.Status)
			assert.Equal(t, legacyfood.Pending, view.Order.ActivityPayment.Status)
			var pending legacyfood.Command
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT command FROM bot.food_pending WHERE owner='alice'`).Scan(&pending),
			)
			assert.Equal(t, legacyfood.Meals, pending.Kind)
			assert.Equal(t, view.Order.ID, pending.OrderID)
			assert.Equal(t, view.Order.Version, pending.Version)
			assert.Equal(t, view.Order.MealPayment.Generation, pending.Generation)
			var saved int
			require.NoError(t, f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.interactions i,
 jsonb_array_elements(i.content) r,jsonb_array_elements(r->'calls') c
 WHERE i.owner='alice' AND i.update_id=1989 AND i.kind='script_runs'
 AND c->'food'->>'event_id'='food-bot' AND c->'food'->>'key'<>''`).Scan(&saved))
			assert.Equal(t, 4, saved)
		})
	}
}

func TestScriptFoodRejectsUngroundedForeignAndStaleWrites(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	foreign, err := service.Execute(
		t.Context(),
		"bob",
		legacyfood.Command{EventID: "food-bot", Name: "toggle_activity", Activity: "yoga", Key: "foreign"},
	)
	require.NoError(t, err)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				_, callErr := callback(
					ctx,
					scriptclient.ToolCall{Name: "food.change", Arguments: json.RawMessage(`{"name":"delete_meals"}`)},
				)
				require.Error(t, callErr)
				scriptFoodRead(ctx, t, callback)
				for _, args := range []string{
					`{"name":"delete_meals","owner":"bob"}`,
					`{"name":"delete_meals","order_id":"` + foreign.ID + `"}`,
					`{"name":"delete_meals","version":999}`,
					`{"name":"submit_proof","proof_id":"invented"}`,
				} {
					_, callErr = callback(
						ctx,
						scriptclient.ToolCall{Name: "food.change", Arguments: json.RawMessage(args)},
					)
					require.Error(t, callErr)
				}
				_, callErr = service.Execute(
					ctx,
					"alice",
					legacyfood.Command{
						EventID:  "food-bot",
						Name:     "toggle_activity",
						Activity: "cacao",
						Key:      "concurrent",
					},
				)
				require.NoError(t, callErr)
				_, callErr = callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "food.change",
						Arguments: json.RawMessage(`{"name":"save_meals","meals":` + foodToolMeals + `}`),
					},
				)
				require.Error(t, callErr)
				return json.RawMessage(`{"denied":true}`), nil
			},
		),
	)
	view, err := service.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, view.Order.Version)
	assert.Empty(t, view.Order.Meals)
	assert.True(t, view.Order.Activities["cacao"])
	view, err = service.View(t.Context(), "bob", "food-bot", foreign.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 1, view.Order.Version)
}

func TestScriptFoodRevocationAndUnavailableDiscovery(t *testing.T) {
	t.Parallel()
	for _, revoke := range []string{
		`UPDATE core.users SET can_book=false WHERE id='alice'`,
		`UPDATE core.pass_events SET finishes_at=now()-interval '1 day' WHERE id='food-bot'`,
	} {
		t.Run(revoke, func(t *testing.T) {
			t.Parallel()
			f, _ := foodBotFixture(t)
			runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						scriptFoodRead(ctx, t, callback)
						_, err := f.db.Exec(ctx, revoke)
						require.NoError(t, err)
						list := scriptCall(ctx, t, callback, "$list", `{}`)
						assert.NotContains(t, string(list), "food.")
						for _, call := range []scriptclient.ToolCall{
							{Name: "$help", Arguments: json.RawMessage(`{"name":"food.change"}`)},
							{Name: "food.change", Arguments: json.RawMessage(`{"name":"toggle_activity","activity":"yoga"}`)},
						} {
							_, err = callback(ctx, call)
							require.Error(t, err)
						}
						return json.RawMessage(`{"denied":true}`), nil
					},
				),
			)
			var count int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM core.food_orders WHERE owner='alice'`).Scan(&count),
			)
			assert.Zero(t, count)
		})
	}
}
