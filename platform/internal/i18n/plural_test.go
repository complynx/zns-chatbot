package i18n_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestTranslateCount(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		count string
		en    string
		ru    string
	}{
		{"0", "0 places", "0 мест"},
		{"1", "1 place", "1 место"},
		{"2", "2 places", "2 места"},
		{"5", "5 places", "5 мест"},
		{"11", "11 places", "11 мест"},
		{"21", "21 places", "21 место"},
		{"22", "22 places", "22 места"},
		{"25", "25 places", "25 мест"},
		{"101", "101 places", "101 место"},
		{"111", "111 places", "111 мест"},
		{"0.0", "0.0 places", "0,0 места"},
		{"1.0", "1.0 places", "1,0 места"},
		{"1.00", "1.00 places", "1,00 места"},
		{"1.5", "1.5 places", "1,5 места"},
		{"2.25", "2.25 places", "2,25 места"},
		{"-1", "-1 place", "-1 место"},
		{"-2", "-2 places", "-2 места"},
		{"-5", "-5 places", "-5 мест"},
		{"-21", "-21 places", "-21 место"},
		{"-1.0", "-1.0 places", "-1,0 места"},
		{"-1.5", "-1.5 places", "-1,5 места"},
		{"1001", "1,001 places", "1\u00a0001 место"},
		{"0001", "1 place", "1 место"},
	} {
		t.Run(test.count, func(t *testing.T) {
			t.Parallel()
			en, err := i18n.TranslateCount("en-US", i18n.Places, test.count)
			require.NoError(t, err)
			require.Equal(t, test.en, en)
			ru, err := i18n.TranslateCount("ru-RU", i18n.Places, test.count)
			require.NoError(t, err)
			require.Equal(t, test.ru, ru)
		})
	}
}

func TestTranslateMealDays(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		count string
		want  string
	}{
		{"1", "Питание: 1 день"},
		{"2", "Питание: 2 дня"},
		{"5", "Питание: 5 дней"},
		{"1.5", "Питание: 1,5 дня"},
	} {
		text, err := i18n.TranslateCount("ru", i18n.MealDays, test.count)
		require.NoError(t, err)
		require.Equal(t, test.want, text)
	}
	text, err := i18n.TranslateCount("de", i18n.MealDays, "21")
	require.NoError(t, err)
	require.Equal(t, "Meals: 21 days", text)
	text, err = i18n.TranslateCount("en", i18n.MealDays, "1")
	require.NoError(t, err)
	require.Equal(t, "Meals: 1 day", text)
}

func TestTranslateCountRejectsUnknownMessage(t *testing.T) {
	t.Parallel()
	text, err := i18n.TranslateCount("en", i18n.CountID("missing"), "1")
	require.ErrorIs(t, err, i18n.ErrUnknownMessage)
	require.Empty(t, text)
}
