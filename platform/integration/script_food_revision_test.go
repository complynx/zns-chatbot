package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/identity"
	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestScriptFoodRepeatedPreparationUsesLatestHint(t *testing.T) {
	t.Parallel()
	for _, last := range []string{legacyfood.Meals, legacyfood.Activity} {
		t.Run(last, func(t *testing.T) {
			t.Parallel()
			f, service := foodBotFixture(t)
			runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						scriptFoodRead(ctx, t, callback)
						scriptCall(ctx, t, callback, "food.change", `{"name":"save_meals","meals":`+foodToolMeals+`}`)
						scriptCall(ctx, t, callback, "food.change", `{"name":"toggle_activity","activity":"yoga"}`)
						scriptCall(ctx, t, callback, "food.payment.prepare", `{"kind":"meals"}`)
						scriptCall(ctx, t, callback, "food.payment.prepare", `{"kind":"`+last+`"}`)
						return json.RawMessage(`{"prepared":true}`), nil
					},
				),
			)
			handle(t, f.b, message(1989, identity.AliceTelegramID, "Read my saved history and orders"))
			view, err := service.View(t.Context(), "alice", "food-bot", "")
			require.NoError(t, err)
			var pending legacyfood.Command
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT command FROM bot.food_pending WHERE owner='alice'`).Scan(&pending),
			)
			assert.Equal(t, last, pending.Kind)
			assert.Equal(t, view.Order.Version, pending.Version)
			assert.EqualValues(t, 4, view.Order.Version)
		})
	}
}

func TestScriptFoodObservedCatalogueCannotChangeBeforeSave(t *testing.T) {
	t.Parallel()
	changes := map[string]string{
		"dish":        `UPDATE core.food_events SET menu=jsonb_set(menu,'{friday,lunch,0,title_en}','"Different dish"') WHERE event_id='food-bot'`,
		"item_price":  `UPDATE core.food_events SET menu=jsonb_set(menu,'{friday,lunch,0,price}','999') WHERE event_id='food-bot'`,
		"combo_price": `UPDATE core.food_events SET meal_prices='{"with_soup":999,"without_soup":888}' WHERE event_id='food-bot'`,
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f, _ := foodBotFixture(t)
			// Change only when the already-bound command reaches the API transport.
			f.b.API.HTTP = &http.Client{Transport: foodToolTransport{before: func(ctx context.Context) error {
				_, err := f.db.Exec(ctx, change)
				return err
			}}}
			runScriptReads(
				t,
				f,
				hostScriptFunc(
					func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
						scriptFoodRead(ctx, t, callback)
						scriptCall(ctx, t, callback, "food.quote", `{"meals":`+foodToolMeals+`}`)
						_, err := callback(
							ctx,
							scriptclient.ToolCall{
								Name:      "food.change",
								Arguments: json.RawMessage(`{"name":"save_meals","meals":` + foodToolMeals + `}`),
							},
						)
						require.Error(t, err)
						return json.RawMessage(`{"stale":true}`), nil
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

func TestScriptFoodQuoteRejectsChangedCompletedView(t *testing.T) {
	t.Parallel()
	f, _ := foodBotFixture(t)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				scriptFoodRead(ctx, t, callback)
				_, err := f.db.Exec(
					ctx,
					`UPDATE core.food_events SET activity_prices=jsonb_set(activity_prices,'{yoga}','999') WHERE event_id='food-bot'`,
				)
				require.NoError(t, err)
				_, err = callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "food.quote",
						Arguments: json.RawMessage(`{"meals":` + foodToolMeals + `}`),
					},
				)
				require.Error(t, err)
				scriptFoodRead(ctx, t, callback)
				scriptCall(ctx, t, callback, "food.quote", `{"meals":`+foodToolMeals+`}`)
				return json.RawMessage(`{"refreshed":true}`), nil
			},
		),
	)
}

func TestScriptFoodCatalogueRevisionPreservesReplayAndManualCommands(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	view, err := service.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	revision, err := legacyfood.CatalogRevision(view.Event)
	require.NoError(t, err)
	command := legacyfood.Command{
		EventID:         view.Event.ID,
		Key:             "bound",
		Name:            "toggle_activity",
		Activity:        "yoga",
		CatalogRevision: revision,
	}
	order, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	_, err = f.db.Exec(
		t.Context(),
		`UPDATE core.food_events SET activity_prices=jsonb_set(activity_prices,'{yoga}','999') WHERE event_id='food-bot'`,
	)
	require.NoError(t, err)
	replayed, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, order.Version, replayed.Version)
	assert.Equal(t, order.ActivityTotal, replayed.ActivityTotal)
	command.Key, command.OrderID, command.Version = "new-bound", order.ID, order.Version
	_, err = service.Execute(t.Context(), "alice", command)
	require.Error(t, err)
	command.Key, command.CatalogRevision = "manual", ""
	manual, err := service.Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.Equal(t, order.Version+1, manual.Version)
	assert.False(t, manual.Activities["yoga"])
}
