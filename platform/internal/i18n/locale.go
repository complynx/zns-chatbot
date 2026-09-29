package i18n

import (
	"slices"
	"strings"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
)

// Locale is a canonical presentation language code, independent of payment country.
type Locale string

const (
	English Locale = "en"
	Russian Locale = "ru"
)

type localeSpec struct {
	tag         language.Tag
	decimalMark string
	minusSign   string
	cardinals   []plural.Form
	catalog     func() translations
}

// Register each supported language once, with its CLDR metadata and catalog.
func localeRegistry() map[Locale]localeSpec {
	return map[Locale]localeSpec{
		English: {
			tag: language.English, decimalMark: ".", minusSign: "-", catalog: englishCatalog,
			cardinals: []plural.Form{plural.One, plural.Other},
		},
		Russian: {
			tag: language.Russian, decimalMark: ",", minusSign: "-", catalog: russianCatalog,
			cardinals: []plural.Form{plural.One, plural.Few, plural.Many, plural.Other},
		},
	}
}

// SupportedLocales returns canonical codes in stable lexical order.
func SupportedLocales() []Locale {
	registry := localeRegistry()
	locales := make([]Locale, 0, len(registry))
	for locale := range registry {
		locales = append(locales, locale)
	}
	slices.Sort(locales)
	return locales
}

// IsSupported validates an exact canonical preference code. Use NormalizeLocale
// for Telegram tags; do not use fallback to validate explicit selections.
func IsSupported(locale string) bool {
	_, exists := localeRegistry()[Locale(locale)]
	return exists
}

// FallbackLocales returns canonical exact tag, base language, explicitly declared
// compatible languages, and English, without duplicates. Candidates may not have
// a product catalog; callers can use the same chain for localized event content.
// Invalid tags yield English. Legacy by/ua aliases mean Belarusian/Ukrainian.
func FallbackLocales(raw string) []Locale {
	aliases := map[string]string{"by": "be", "ua": "uk"}
	compatible := map[string][]Locale{"be": {Russian}, "uk": {Russian}}
	raw = strings.ReplaceAll(strings.TrimSpace(raw), "_", "-")
	baseInput, rest, hasRest := strings.Cut(strings.ToLower(raw), "-")
	if alias, ok := aliases[baseInput]; ok {
		raw = alias
		if hasRest {
			raw += "-" + rest
		}
	}
	tag, err := language.Parse(raw)
	if err != nil || tag == language.Und {
		return []Locale{English}
	}
	base, _, _ := tag.Raw()
	chain := []Locale{Locale(tag.String()), Locale(base.String())}
	chain = append(chain, compatible[base.String()]...)
	chain = append(chain, English)
	result := make([]Locale, 0, len(chain))
	for _, locale := range chain {
		if !slices.Contains(result, locale) {
			result = append(result, locale)
		}
	}
	return result
}

// NormalizeLocale resolves a BCP 47 tag to the first supported fallback locale.
func NormalizeLocale(raw string) Locale {
	return resolveLocale(raw, localeRegistry())
}

func resolveLocale(raw string, registry map[Locale]localeSpec) Locale {
	for _, locale := range FallbackLocales(raw) {
		if _, exists := registry[locale]; exists {
			return locale
		}
	}
	return English
}
