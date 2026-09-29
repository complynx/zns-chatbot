// Package i18n renders plain-text product messages from English and Russian catalogs.
package i18n

import (
	"errors"
	"fmt"
	"strings"
)

// ID identifies a product message independently of its translated text.
type ID string

var (
	ErrUnknownMessage = errors.New("unknown message")
	ErrMissingValue   = errors.New("missing message value")
)

// Translate substitutes named {values} once, without interpreting their contents.
// Missing values and unknown IDs return an error and no partial message. Empty
// values are valid; unused values are ignored. Callers own formatting and escaping
// for surfaces that interpret markup; this function produces plain text.
func Translate(locale string, id ID, values map[string]string) (string, error) {
	message, err := lookupMessage(locale, id, localeRegistry())
	if err != nil {
		return "", err
	}
	return interpolate(id, message, values)
}

func interpolate(id ID, message string, values map[string]string) (string, error) {
	var result strings.Builder
	for {
		before, tail, found := strings.Cut(message, "{")
		result.WriteString(before)
		if !found {
			return result.String(), nil
		}
		name, after, _ := strings.Cut(tail, "}")
		value, present := values[name]
		if !present {
			return "", fmt.Errorf("%w: %s in %s", ErrMissingValue, name, id)
		}
		result.WriteString(value)
		message = after
	}
}

func lookupMessage(raw string, id ID, registry map[Locale]localeSpec) (string, error) {
	for _, locale := range FallbackLocales(raw) {
		if spec, exists := registry[locale]; exists {
			if text := spec.catalog().text[id]; text != "" {
				return text, nil
			}
		}
	}
	return "", unknownMessage(string(id))
}

func unknownMessage(id string) error {
	return fmt.Errorf("%w: %s", ErrUnknownMessage, id)
}
