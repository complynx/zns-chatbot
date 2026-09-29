package bot

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/orders"
)

func TestModernChoiceMealReferencesAvoidLargeArguments(t *testing.T) {
	t.Parallel()
	name := strings.Repeat("dish", 50000)
	menu := orders.Catalog{
		Dishes:  map[string]orders.Definition{name: {Price: 100}},
		Choices: map[string]map[string]map[string][]string{"day": {"meal": {"category": {name}}}},
	}
	raw, err := json.Marshal(menu)
	require.NoError(t, err)
	event := orders.Event{Menu: raw, Extras: map[string]orders.Extra{}}
	before, err := orders.Canonicalize(orders.ChoiceInput{}, menu, event.Extras)
	require.NoError(t, err)
	patch := modernChoiceArguments{
		Meals: []modernChoiceMeal{{Day: 0, Meal: 0, Dishes: []modernChoiceDish{{Ref: 0, Count: 1}}}},
	}
	args, err := json.Marshal(patch)
	require.NoError(t, err)
	assert.Less(t, len(args), 256)
	after, err := patchModernChoice(before, event, patch)
	require.NoError(t, err)
	require.Len(t, after.Days["day"].Mealtimes["meal"].Dishes, 1)
	assert.Equal(t, name, after.Days["day"].Mealtimes["meal"].Dishes[0].Name)
	labels, err := modernChoiceMealCatalog(event)
	require.NoError(t, err)
	assert.True(t, labels[0].Meals[0].Dishes[0].Partial)
	assert.LessOrEqual(t, len([]rune(labels[0].Meals[0].Dishes[0].Label)), modernChoiceLabelRunes)
	patch.Meals[0].Append = true
	_, err = patchModernChoice(after, event, patch)
	require.Error(t, err, "canonical256KiB domain bound still applies")
	patch.Meals = []modernChoiceMeal{{Day: 0, Meal: 0, Remove: true}}
	after, err = patchModernChoice(after, event, patch)
	require.NoError(t, err)
	assert.Empty(t, after.Days["day"].Mealtimes)
	patch.Meals = []modernChoiceMeal{{Day: 0, Meal: -1, Remove: true}}
	after, err = patchModernChoice(after, event, patch)
	require.NoError(t, err)
	assert.Empty(t, after.Days)
}
