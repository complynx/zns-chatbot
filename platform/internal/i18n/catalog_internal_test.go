package i18n

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"golang.org/x/text/number"
)

func TestCatalogParity(t *testing.T) {
	t.Parallel()
	registry := localeRegistry()
	fallback := registry[English].catalog()
	for locale, spec := range registry {
		t.Run(string(locale), func(t *testing.T) {
			t.Parallel()
			catalog := spec.catalog()
			printer := message.NewPrinter(spec.tag)
			zero := printer.Sprintf("%d", 0)
			require.Equal(t, zero+spec.decimalMark+zero, printer.Sprint(number.Decimal(0, number.Scale(1))))
			require.Equal(t, spec.minusSign+printer.Sprintf("%d", 1), printer.Sprintf("%d", -1))
			require.Len(t, catalog.text, len(fallback.text))
			require.Len(t, catalog.counts, len(fallback.counts))
			for id, english := range fallback.text {
				require.NotEmpty(t, catalog.text[id], id)
				assertPlaceholders(t, id, english, catalog.text[id])
			}
			for id := range fallback.counts {
				for _, form := range spec.cardinals {
					text := catalog.counts[id].selectForm(form)
					require.NotEmpty(t, text, "%s/%s/%v", locale, id, form)
					assertPlaceholders(t, ID(id), "{count}", text)
				}
			}
		})
	}
}

func assertPlaceholders(t *testing.T, id ID, reference, template string) {
	t.Helper()
	placeholder := regexp.MustCompile(`\{([a-z]+)\}`)
	names := placeholder.FindAllString(reference, -1)
	require.ElementsMatch(t, names, placeholder.FindAllString(template, -1), id)
	values := map[string]string{}
	for _, name := range names {
		values[strings.Trim(name, "{}")] = "test"
	}
	text, err := interpolate(id, template, values)
	require.NoError(t, err)
	require.NotContains(t, text, "{")
	require.NotContains(t, text, "}")
}

func TestMissingTranslationsFallback(t *testing.T) {
	t.Parallel()
	registry := localeRegistry()
	russian := registry[Russian]
	catalog := russian.catalog()
	delete(catalog.text, PaymentMethods)
	delete(catalog.counts, Places)
	russian.catalog = func() translations { return catalog }
	registry[Russian] = russian
	text, err := lookupMessage("ru", PaymentMethods, registry)
	require.NoError(t, err)
	require.Equal(t, "Payment methods", text)
	value, err := parseDecimal("21")
	require.NoError(t, err)
	text, err = renderCount("ru", Places, value, registry)
	require.NoError(t, err)
	require.Equal(t, "21 places", text, "fallback must use English grammar, not Russian one")
	catalog.counts[Places] = pluralForms{one: "{count} место", many: "{count} мест", other: "{count} места"}
	value, err = parseDecimal("22")
	require.NoError(t, err)
	text, err = renderCount("ru", Places, value, registry)
	require.NoError(t, err)
	require.Equal(t, "22 places", text, "missing individual plural form falls back")
	text, err = lookupMessage("ru", ID("unknown"), registry)
	require.ErrorIs(t, err, ErrUnknownMessage)
	require.Empty(t, text)
}

func TestNewLocaleWinsBeforeCompatibleFallback(t *testing.T) {
	t.Parallel()
	registry := localeRegistry()
	ukrainian := registry[Russian]
	ukrainian.tag = language.Ukrainian
	catalog := ukrainian.catalog()
	catalog.text[PaymentMethods] = "Способи оплати"
	delete(catalog.text, PaymentCashChoice)
	ukrainian.catalog = func() translations { return catalog }
	registry[Locale("uk")] = ukrainian
	require.Equal(t, Locale("uk"), resolveLocale("ua", registry))
	require.Equal(t, Locale("uk"), resolveLocale("uk-UA", registry))
	text, err := lookupMessage("uk-UA", PaymentMethods, registry)
	require.NoError(t, err)
	require.Equal(t, "Способи оплати", text)
	text, err = lookupMessage("uk-UA", PaymentCashChoice, registry)
	require.NoError(t, err)
	require.Equal(t, registry[Russian].catalog().text[PaymentCashChoice], text)
	regional := ukrainian
	regional.catalog = englishCatalog
	registry[Locale("uk-UA")] = regional
	require.Equal(t, Locale("uk-UA"), resolveLocale("ua-UA", registry))
	text, err = lookupMessage("uk-UA", PaymentMethods, registry)
	require.NoError(t, err)
	require.Equal(t, "Payment methods", text, "exact regional catalog wins")
}
