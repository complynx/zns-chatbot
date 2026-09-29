package i18n

import "golang.org/x/text/feature/plural"

// CountID identifies a message that requires a count and CLDR form selection.
type CountID string

const (
	MealDays CountID = "order.meal_days"
	Places   CountID = "booking.places"
)

type pluralForms struct {
	zero  string
	one   string
	two   string
	few   string
	many  string
	other string
}

// TranslateCount selects a CLDR cardinal form using the exact visible decimal
// count, then inserts its localized number. Use [strconv.FormatInt] for integer
// counts. "1" and "1.0" differ in CLDR; negative counts use their absolute value
// for grammar and retain the sign in output. Domain code must reject quantities
// that cannot be booked; rendering does not authorize a quantity.
func TranslateCount(locale string, id CountID, count string) (string, error) {
	value, err := parseDecimal(count)
	if err != nil {
		return "", err
	}
	return renderCount(locale, id, value, localeRegistry())
}

func renderCount(raw string, id CountID, value decimal, registry map[Locale]localeSpec) (string, error) {
	for _, locale := range FallbackLocales(raw) {
		spec, exists := registry[locale]
		if !exists {
			continue
		}
		form := plural.Cardinal.MatchDigits(spec.tag, value.digits, value.exponent, len(value.fraction))
		text := spec.catalog().counts[id].selectForm(form)
		if text != "" {
			return interpolate(ID(id), text, map[string]string{"count": value.format(spec)})
		}
	}
	return "", unknownMessage(string(id))
}

func (p pluralForms) selectForm(form plural.Form) string {
	switch form {
	case plural.Zero:
		return p.zero
	case plural.One:
		return p.one
	case plural.Two:
		return p.two
	case plural.Few:
		return p.few
	case plural.Many:
		return p.many
	case plural.Other:
		return p.other
	default:
		return ""
	}
}
