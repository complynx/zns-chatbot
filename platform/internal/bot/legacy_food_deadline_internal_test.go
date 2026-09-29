package bot

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/complynx/zns-chatbot/platform/internal/legacyfood"
)

func TestFoodMealEntryDeadlineVisibility(t *testing.T) {
	t.Parallel()
	now := time.Now()
	for _, status := range []string{legacyfood.Pending, legacyfood.Rejected, legacyfood.Submitted, legacyfood.Paid} {
		t.Run(status, func(t *testing.T) {
			t.Parallel()
			view := legacyfood.View{
				Event: legacyfood.Event{Deadline: now.Add(-time.Hour)},
				Order: legacyfood.Order{
					MealTotal:   18500,
					Meals:       legacyfood.MealSelection{"friday": {}},
					MealPayment: legacyfood.Payment{Status: status},
				},
			}
			assert.Equal(t, status == legacyfood.Submitted || status == legacyfood.Paid, foodMealEntry(view, now))
			view.Order.MealTotal = 0
			assert.False(t, foodMealEntry(view, now))
			view.Event.Deadline = now.Add(time.Hour)
			assert.True(t, foodMealEntry(view, now))
		})
	}
}
