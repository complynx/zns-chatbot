package integration_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/scriptclient"
)

func TestFoodArchivedAdmittedPaymentPrepare(t *testing.T) {
	t.Parallel()
	f, s := foodBotFixture(t)
	order, err := s.Execute(
		t.Context(),
		"alice",
		legacyfood.Command{EventID: "food-bot", Name: "toggle_activity", Activity: "yoga", Key: "qa-activity"},
	)
	require.NoError(t, err)
	foodAdmissionSwitch(
		t,
		f,
		"begin_payment",
		`UPDATE core.pass_events SET finishes_at='2000-01-01Z' WHERE id='food-bot';`,
	)
	runScriptReads(
		t,
		f,
		hostScriptFunc(
			func(ctx context.Context, _ []scriptclient.Tool, callback scriptclient.Callback) (json.RawMessage, error) {
				scriptFoodRead(ctx, t, callback)
				_, callErr := callback(
					ctx,
					scriptclient.ToolCall{
						Name:      "food.payment.prepare",
						Arguments: json.RawMessage(`{"kind":"activities"}`),
					},
				)
				current, readErr := s.Get(t.Context(), "alice", order.EventID, order.ID)
				require.NoError(t, readErr)
				t.Logf(
					"prepare error=%v oldVersion=%d newVersion=%d receiver=%q",
					callErr,
					order.Version,
					current.Version,
					current.PaymentAdmin,
				)
				require.Error(t, callErr, "archived original event must deny newly admitted owner payment preparation")
				require.Equal(t, order.Version, current.Version)
				return json.RawMessage(`{}`), nil
			},
		),
	)
}

func TestFoodPaymentArchivePreservesCompletedReplay(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, action, kind string }{
		{"meals", "begin_payment", legacyfood.Meals},
		{"activities", "begin_payment", legacyfood.Activity},
		{"prepare-activities", "prepare_activities", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, service := foodBotFixture(t)
			order, err := service.Execute(t.Context(), "alice", legacyfood.Command{
				EventID: "food-bot", Name: "toggle_activity", Activity: "yoga", Key: "activity",
			})
			require.NoError(t, err)
			var meals legacyfood.MealSelection
			require.NoError(t, json.Unmarshal([]byte(foodToolMeals), &meals))
			order, err = service.Execute(t.Context(), "alice", legacyfood.Command{
				EventID: order.EventID, OrderID: order.ID, Version: order.Version,
				Name: "save_meals", Meals: meals, Key: "meals",
			})
			require.NoError(t, err)
			command := legacyfood.Command{
				EventID: order.EventID, OrderID: order.ID, Version: order.Version,
				Name: test.action, Kind: test.kind, Key: "prepare-active",
			}
			completed, err := service.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.pass_events SET finishes_at='2000-01-01Z' WHERE id='food-bot'`)
			require.NoError(t, err)
			replayed, err := service.Execute(t.Context(), "alice", command)
			require.NoError(t, err)
			completed.CreatedAt, replayed.CreatedAt = completed.CreatedAt.UTC(), replayed.CreatedAt.UTC()
			require.NotNil(t, completed.LastUpdated)
			require.NotNil(t, replayed.LastUpdated)
			completedUpdated, replayedUpdated := completed.LastUpdated.UTC(), replayed.LastUpdated.UTC()
			completed.LastUpdated, replayed.LastUpdated = &completedUpdated, &replayedUpdated
			require.Equal(t, completed, replayed)
			command.Key, command.Version = "prepare-archived", completed.Version
			_, err = service.Execute(t.Context(), "alice", command)
			requireCode(t, err, "food_event_inactive")
			current, err := service.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			require.Equal(t, completed.Version, current.Version)
			require.Equal(t, completed.PaymentAdmin, current.PaymentAdmin)
		})
	}
}
