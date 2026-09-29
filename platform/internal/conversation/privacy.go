package conversation

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// These markers suppress archive copies, not business intent. The current
// request still goes through the ordinary agent; no keyword routes an action.
var sensitiveText = regexp.MustCompile(
	`(?i)(passport|паспорт|password|пароль|api[ _-]?key|bearer\s+|secret\s*[:=]|token\s*[:=]|sk-[a-z0-9_-]{12,}|[a-z][a-z0-9+.-]*://[^\s/]+:[^\s/]+@)`,
)

var identityText = regexp.MustCompile(
	`(?i)(my\s+(legal\s+)?name\s+is|меня\s+зовут|мо[её]\s+имя|nazywam\s+si[eę]|ich\s+hei[ßs]e|mein\s+name\s+ist)`,
)

func Sanitize(text string) (string, bool) {
	text, omitted := sanitizePrivateText(text)
	if omitted {
		return text, true
	}
	return boundText(text)
}

func sanitizePrivateText(text string) (string, bool) {
	if sensitiveText.MatchString(text) || identityText.MatchString(text) {
		return "[sensitive text omitted]", true
	}
	return strings.ReplaceAll(text, "\x00", ""), false
}

func boundText(text string) (string, bool) {
	if len(text) <= MaxTextBytes {
		return text, false
	}
	text = text[:MaxTextBytes]
	for !utf8.ValidString(text) {
		text = text[:len(text)-1]
	}
	return text, true
}
