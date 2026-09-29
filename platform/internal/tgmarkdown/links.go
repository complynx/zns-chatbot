package tgmarkdown

import (
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/yuin/goldmark/util"
)

func markdownText(value []byte) string {
	return string(util.ResolveEntityNames(util.ResolveNumericReferences(util.UnescapePunctuations(value))))
}

// Preserve the parsed Markdown destination. Escape only Telegram's target
// delimiters; do not reserialize URLs, drop query parameters, or fetch them.
func linkTarget(raw []byte) (string, error) {
	target := markdownText(raw)
	if !SafeURL(target) {
		return "", ErrUnsafeURL
	}
	return escapeCharacters(target, ")\\"), nil
}

// SafeURL checks an already decoded link target without changing it.
func SafeURL(target string) bool {
	for _, r := range target {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.User != nil || parsed.Opaque != "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "http":
		if parsed.Hostname() == "" {
			return false
		}
	case "tg":
		if parsed.Host != "user" || parsed.Path != "" || parsed.Fragment != "" {
			return false
		}
		query, queryErr := url.ParseQuery(parsed.RawQuery)
		id, idErr := strconv.ParseInt(query.Get("id"), 10, 64)
		if queryErr != nil || idErr != nil || id <= 0 || len(query) != 1 || len(query["id"]) != 1 {
			return false
		}
	default:
		return false
	}
	return true
}
