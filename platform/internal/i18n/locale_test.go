package i18n_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/i18n"
)

func TestSupportedLocales(t *testing.T) {
	t.Parallel()
	locales := i18n.SupportedLocales()
	require.Contains(t, locales, i18n.English)
	require.Contains(t, locales, i18n.Russian)
	for _, locale := range locales {
		require.True(t, i18n.IsSupported(string(locale)))
		require.Equal(t, locale, i18n.NormalizeLocale(string(locale)))
	}
	for _, invalid := range []string{"", "unknown", "RU", " ru ", "ru-RU", "ua", "by"} {
		require.False(t, i18n.IsSupported(invalid), invalid)
	}
}

func TestFallbackLocales(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input string
		chain []i18n.Locale
		want  i18n.Locale
	}{
		{"be", []i18n.Locale{"be", "ru", "en"}, i18n.Russian},
		{"by", []i18n.Locale{"be", "ru", "en"}, i18n.Russian},
		{"be-BY", []i18n.Locale{"be-BY", "be", "ru", "en"}, i18n.Russian},
		{"by-BY", []i18n.Locale{"be-BY", "be", "ru", "en"}, i18n.Russian},
		{"uk", []i18n.Locale{"uk", "ru", "en"}, i18n.Russian},
		{"ua", []i18n.Locale{"uk", "ru", "en"}, i18n.Russian},
		{"uk-UA", []i18n.Locale{"uk-UA", "uk", "ru", "en"}, i18n.Russian},
		{"pl", []i18n.Locale{"pl", "en"}, i18n.English},
		{"pl-PL", []i18n.Locale{"pl-PL", "pl", "en"}, i18n.English},
		{"ru-RU", []i18n.Locale{"ru-RU", "ru", "en"}, i18n.Russian},
		{"en-US", []i18n.Locale{"en-US", "en"}, i18n.English},
		{"en", []i18n.Locale{"en"}, i18n.English},
		{"", []i18n.Locale{"en"}, i18n.English},
		{"invalid!", []i18n.Locale{"en"}, i18n.English},
	} {
		t.Run(test.input, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, test.chain, i18n.FallbackLocales(test.input))
			require.Equal(t, test.want, i18n.NormalizeLocale(test.input))
		})
	}
}
