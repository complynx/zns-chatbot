package broadcasts

import (
	"regexp"
	"strings"
)

var linkPattern = regexp.MustCompile(`(?i)(?:[a-z][a-z0-9+.-]*://|mailto:|tel:)[^\s<>"']+|` +
	`[\p{L}\p{N}._%+-]+@[\p{L}\p{N}.-]+\.[\p{L}]{2,63}|` +
	`(?:[\p{L}\p{N}-]+\.)+[\p{L}]{2,63}(?::[0-9]+)?(?:[/?#][^\s<>"']*)?|` +
	`\b[0-9]{1,3}(?:\.[0-9]{1,3}){3}\b(?::[0-9]+)?(?:[/?#][^\s<>"']*)?|` +
	`@[a-z0-9_]+`)

// PreservesLinks rejects new or modified addresses and mentions in a rewrite
// that has seen private memory. Telegram previews are disabled separately.
func PreservesLinks(original, rendered string) bool {
	allowed := map[string]bool{}
	for _, link := range linkPattern.FindAllString(original, -1) {
		allowed[strings.TrimRight(link, ".,;:!?)]}")] = true
	}
	for _, link := range linkPattern.FindAllString(rendered, -1) {
		if !allowed[strings.TrimRight(link, ".,;:!?)]}")] {
			return false
		}
	}
	return true
}
