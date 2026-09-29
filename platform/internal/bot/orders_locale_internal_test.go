package bot

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrderMealDaysUseLocaleCardinalRules(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		language string
		count    int
		want     string
	}{
		{"en", 0, "Meals: 0 days"}, {"en", 1, "Meals: 1 day"}, {"en", 2, "Meals: 2 days"},
		{"ru", 1, "Питание: 1 день"}, {"ru", 2, "Питание: 2 дня"}, {"ru", 5, "Питание: 5 дней"},
		{"ru", 11, "Питание: 11 дней"}, {"ru", 21, "Питание: 21 день"}, {"ru", 22, "Питание: 22 дня"},
	} {
		messages := orderMessages{language: test.language}
		assert.Equal(t, test.want, messages.mealDays(test.count))
		require.NoError(t, messages.err)
	}
}
