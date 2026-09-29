package identity

import (
	"errors"
	"fmt"
	"strings"
)

// SyntheticEmail matches zitadeltg's numeric-id lookup convention. It is not
// proof of mailbox ownership and must not enable automatic email account linking.
func SyntheticEmail(botID, userID int64, domain string) (string, error) {
	const maxTelegramID = 1<<52 - 1
	if botID <= 0 || userID <= 0 || userID > maxTelegramID || botID > maxTelegramID ||
		!strings.HasSuffix(domain, ".invalid") || len(domain) > 253 || strings.ContainsAny(domain, "@+/: \t\r\n") {
		return "", errors.New("invalid synthetic identity")
	}
	for label := range strings.SplitSeq(domain, ".") {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", errors.New("invalid synthetic identity")
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return "", errors.New("invalid synthetic identity")
			}
		}
	}
	return fmt.Sprintf("tg+%d+%d@%s", botID, userID, domain), nil
}
