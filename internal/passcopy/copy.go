// Package passcopy shares the localized pass-registration messages between
// Telegram and the Mini App.
package passcopy

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed messages.json
var messagesJSON []byte

var messages = readMessages() //nolint:gochecknoglobals // Decode the embedded read-only translations once.

func readMessages() map[string][2]string {
	var catalog map[string][2]string
	if err := json.Unmarshal(messagesJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog
}

// Text returns the localized message, replacing each %s with its next argument.
// Russian locale variants select Russian. Other locales select English.
func Text(language, key string, args ...any) string {
	message, exists := messages[key]
	if !exists {
		return key
	}
	locale, _, _ := strings.Cut(strings.ReplaceAll(language, "_", "-"), "-")
	template := message[0]
	if strings.EqualFold(locale, "ru") {
		template = message[1]
	}

	var result strings.Builder
	for _, arg := range args {
		before, after, found := strings.Cut(template, "%s")
		if !found {
			break
		}
		result.WriteString(before)
		_, _ = fmt.Fprint(&result, arg)
		template = after
	}
	result.WriteString(template)
	return result.String()
}

// BrowserScript exposes the same catalog through window.passCopy.
func BrowserScript() []byte {
	return []byte(`(() => {
    const messages = ` + string(messagesJSON) + `;
    window.passCopy = (language, key, ...args) => {
        if (!Object.prototype.hasOwnProperty.call(messages, key)) return key;
        const locale = String(language).toLowerCase().split(/[-_]/)[0];
        let index = 0;
        return messages[key][locale === "ru" ? 1 : 0].replaceAll("%s", () => (
            index < args.length ? String(args[index++]) : "%s"
        ));
    };
})();
`)
}
