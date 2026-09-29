package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

type foodToolTransport struct {
	before    func(context.Context) error
	loseReply bool
}

func (transport foodToolTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path != "/v1/food/commands" {
		return http.DefaultTransport.RoundTrip(request)
	}
	if transport.before != nil {
		if err := transport.before(request.Context()); err != nil {
			return nil, err
		}
	}
	response, err := http.DefaultTransport.RoundTrip(request)
	if err == nil && transport.loseReply {
		_ = response.Body.Close()
		return nil, errors.New("simulated lost food command response")
	}
	return response, err
}

func TestScriptFoodExecutionRechecksPermissionAfterBinding(t *testing.T) {
	t.Parallel()
	f, _ := foodBotFixture(t)
	f.b.API.HTTP = &http.Client{Transport: foodToolTransport{before: func(ctx context.Context) error {
		_, err := f.db.Exec(ctx, `UPDATE core.users SET can_book=false WHERE id='alice'`)
		return err
	}}}
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				scriptFoodRead(ctx, t, callback)
				_, err := callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "food.change",
						Arguments: json.RawMessage(`{"name":"toggle_activity","activity":"yoga"}`),
					},
				)
				require.Error(t, err)
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
}

func TestScriptFoodLostResponseRetainsOriginalCommand(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	f.b.API.HTTP = &http.Client{Transport: foodToolTransport{loseReply: true}}
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				scriptFoodRead(ctx, t, callback)
				_, err := callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "food.change",
						Arguments: json.RawMessage(`{"name":"toggle_activity","activity":"yoga"}`),
					},
				)
				require.Error(t, err)
				return json.RawMessage(`{"uncertain":true}`), nil
			},
		),
	)
	var command legacyfood.Command
	require.NoError(t, f.db.QueryRow(t.Context(), `SELECT c->'food' FROM bot.interactions i,
 jsonb_array_elements(i.content) r,jsonb_array_elements(r->'calls') c
 WHERE i.owner='alice' AND i.update_id=1989 AND i.kind='script_runs' AND c->'food'->>'name'='toggle_activity'`).Scan(&command))
	require.NotEmpty(t, command.Key)
	assert.Equal(t, "food-bot", command.EventID)
	assert.Zero(t, command.Version)
	assert.Empty(t, command.OrderID)
	// A new service instance uses the original durable receipt, not a fresh version.
	replayed, err := (legacyfood.Service{DB: f.db, BotID: 77}).Execute(t.Context(), "alice", command)
	require.NoError(t, err)
	assert.EqualValues(t, 1, replayed.Version)
	assert.True(t, replayed.Activities["yoga"])
	view, err := service.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	assert.EqualValues(t, 1, view.Order.Version)
	_, err = f.db.Exec(t.Context(), `UPDATE core.users SET can_book=false WHERE id='alice'`)
	require.NoError(t, err)
	_, err = service.Execute(t.Context(), "alice", command)
	require.Error(t, err)
}

func TestScriptFoodActivityPreparationAndMealDelete(t *testing.T) {
	t.Parallel()
	f, service := foodBotFixture(t)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				scriptFoodRead(ctx, t, callback)
				scriptCall(ctx, t, callback, "food.change", `{"name":"save_meals","meals":`+foodToolMeals+`}`)
				scriptCall(ctx, t, callback, "food.change", `{"name":"delete_meals"}`)
				scriptCall(ctx, t, callback, "food.change", `{"name":"toggle_activity","activity":"yoga"}`)
				scriptCall(ctx, t, callback, "food.payment.prepare", `{"kind":"activities"}`)
				return json.RawMessage(`{"prepared":"activities"}`), nil
			},
		),
	)
	view, err := service.View(t.Context(), "alice", "food-bot", "")
	require.NoError(t, err)
	assert.Empty(t, view.Order.Meals)
	assert.Zero(t, view.Order.MealTotal)
	assert.EqualValues(t, 75000, view.Order.ActivityTotal)
	var pending legacyfood.Command
	require.NoError(
		t,
		f.db.QueryRow(t.Context(), `SELECT command FROM bot.food_pending WHERE owner='alice'`).Scan(&pending),
	)
	assert.Equal(t, legacyfood.Activity, pending.Kind)
	assert.Equal(t, view.Order.ActivityPayment.Generation, pending.Generation)
	assert.Equal(t, legacyfood.Pending, view.Order.MealPayment.Status)
}
