// Package broadcasts defines the subscription and severity rules for human-authored broadcasts.
package broadcasts

import (
	"errors"
	"strings"
	"unicode/utf16"
)

type Category string

const (
	Advertising Category = "advertising"
	Information Category = "information"
	Warning     Category = "warning"
)

var ErrInvalid = errors.New("invalid broadcast request")

const MaxTextUnits = 2800

// ValidText leaves space for attribution and native confirmation in Telegram's UTF-16 limit.
func ValidText(text string) bool {
	if strings.TrimSpace(text) == "" {
		return false
	}
	units := 0
	for _, r := range text {
		units += utf16.RuneLen(r)
	}
	return units <= MaxTextUnits
}

func (c Category) Valid() bool { return c == Advertising || c == Information || c == Warning }

func (c Category) Optional() bool { return c == Advertising || c == Information }

// NeedsClarification compares the author's category with the agent's assessment.
// The assessment belongs to the exact draft, including its audience and event.
func NeedsClarification(requested, assessed Category) bool {
	return requested == Warning && (assessed == Advertising || assessed == Information) ||
		requested == Information && assessed == Advertising
}
