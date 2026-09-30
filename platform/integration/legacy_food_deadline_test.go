package integration_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
	"github.com/complynx/zns-chatbot/platform/internal/telegram"
)

func TestFoodDeadlineDirectOperationsAndNormalEntry(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			f, s := foodBotFixture(t)
			f.b.WebAppURL = "https://example.invalid"
			_, err := f.db.Exec(t.Context(), `UPDATE core.food_events SET deadline=now()-interval '1 day'`)
			require.NoError(t, err)
			_, err = f.db.Exec(t.Context(), `UPDATE core.users SET language=$1 WHERE id='alice'`, language)
			require.NoError(t, err)
			var meals legacyfood.MealSelection
			require.NoError(t, json.Unmarshal([]byte(foodToolMeals), &meals))
			// Python's direct menu POST/save remains available for an active event.
			order, err := s.Execute(
				t.Context(),
				"alice",
				legacyfood.Command{EventID: "food-bot", Name: "save_meals", Meals: meals, Key: "late-save"},
			)
			require.NoError(t, err)
			_, err = s.Execute(
				t.Context(),
				"alice",
				legacyfood.Command{
					EventID: order.EventID,
					OrderID: order.ID,
					Version: order.Version,
					Name:    "begin_payment",
					Kind:    legacyfood.Meals,
					Key:     "late-pay",
				},
			)
			requireCode(t, err, "food_payment_unavailable")
			original, err := f.b.TG.Send(t.Context(), telegram.Send{ChatID: 101, Text: "Open food"})
			require.NoError(t, err)
			handleVisible(t, f.b, aliceCallback(29300, original.ID, "food|submit_activities"))
			messages := chatMessages(t, f, 101)
			require.NotEmpty(t, messages)
			card := messages[len(messages)-1]
			if language == "en" {
				assert.Contains(t, card.Text, "Meal ordering is closed")
			} else {
				assert.Contains(t, card.Text, "Приём заказов питания закрыт")
			}
			for _, row := range card.Markup.Rows {
				for _, button := range row {
					assert.Nil(t, button.WebApp)
				}
			}
			var mealButtons int
			require.NoError(
				t,
				f.db.QueryRow(t.Context(), `SELECT count(*) FROM bot.food_buttons WHERE owner='alice' AND (command->'command'->>'kind'='meals' OR command->'command'->>'name'='delete_meals')`).
					Scan(&mealButtons),
			)
			assert.Zero(t, mealButtons)
			current, err := s.Get(t.Context(), "alice", order.EventID, order.ID)
			require.NoError(t, err)
			current, err = s.Execute(
				t.Context(),
				"alice",
				legacyfood.Command{
					EventID: order.EventID,
					OrderID: order.ID,
					Version: current.Version,
					Name:    "delete_meals",
					Key:     "late-delete",
				},
			)
			require.NoError(t, err)
			current, err = s.Execute(
				t.Context(),
				"alice",
				legacyfood.Command{
					EventID:  order.EventID,
					OrderID:  order.ID,
					Version:  current.Version,
					Name:     "toggle_activity",
					Activity: "yoga",
					Key:      "late-activity",
				},
			)
			require.NoError(t, err)
			_, err = s.Execute(
				t.Context(),
				"alice",
				legacyfood.Command{
					EventID: order.EventID,
					OrderID: order.ID,
					Version: current.Version,
					Name:    "begin_payment",
					Kind:    legacyfood.Activity,
					Key:     "late-activity-pay",
				},
			)
			require.NoError(t, err)
		})
	}
}
