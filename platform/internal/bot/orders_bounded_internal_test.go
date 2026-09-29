package bot

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestModernOrderLargeCardIsBoundedInBothLocales(t *testing.T) {
	t.Parallel()
	for _, language := range []string{"en", "ru"} {
		t.Run(language, func(t *testing.T) {
			t.Parallel()
			choice := orders.Choice{Extras: map[string]orders.Money{}}
			event := orders.Event{Deadline: time.Now().Add(time.Hour), Extras: map[string]orders.Extra{}}
			for index := range 260 {
				key := fmt.Sprintf("%03d%s", index, strings.Repeat("Ж", 500))
				choice.Extras[key] = 100
				event.Extras[key] = orders.Extra{Price: 100}
			}
			order := orders.Order{ID: "large", State: stateUnpaid, Choice: choice}
			messages := orderMessages{language: language}
			body := orderDescription(order, &messages)
			notice, err := i18n.Translate(language, i18n.OrderMoreDetails, nil)
			require.NoError(t, err)
			assert.Contains(t, body, notice)
			assert.Less(t, len([]rune(body)), 2000)
			keys, complete := orderExtraControlKeys(choice, event)
			assert.Empty(t, keys)
			assert.False(t, complete)
			for _, action := range orderActions(order, event, nil, false, &messages) {
				assert.NotEqual(t, "edit", action.command.Name)
			}
			require.NoError(t, messages.err)
			assert.Len(t, choice.Extras, 260)
		})
	}
}

func TestModernOrderExtraControlsBoundedAndSmallChoicePreserved(t *testing.T) {
	t.Parallel()
	choice := orders.Choice{Extras: map[string]orders.Money{"shuttle": 100}}
	event := orders.Event{Deadline: time.Now().Add(time.Hour), Extras: orders.Extras()}
	keys, complete := orderExtraControlKeys(choice, event)
	assert.True(t, complete)
	assert.Contains(t, keys, "shuttle")
	assert.NotContains(t, keys, "excursion_grodno")
	order := orders.Order{State: stateUnpaid, Choice: choice}
	messages := orderMessages{language: "en"}
	actions := orderActions(order, event, nil, false, &messages)
	remove := false
	for _, action := range actions {
		if action.label == "Remove: Shuttle" {
			remove = true
			assert.NotContains(t, action.command.Choice.Extras, "shuttle")
		}
	}
	assert.True(t, remove)
	for index := range 30 {
		event.Extras[fmt.Sprintf("extra%d", index)] = orders.Extra{Price: 100}
	}
	keys, complete = orderExtraControlKeys(choice, event)
	assert.Len(t, keys, orderCardExtraLimit)
	assert.False(t, complete)
	assert.Equal(t, orders.Money(100), choice.Extras["shuttle"])
}
