package telegram

import (
	"net/url"
	"strings"
)

// PassDraftURL opens a chat with unsubmitted text. The recipient decides whether to send it.
func PassDraftURL(bot User, botID int64, text string) string {
	if botID <= 0 || bot.ID != botID || !bot.IsBot || !partnerBotUsername.MatchString(bot.Username) || text == "" {
		return ""
	}
	return "https://t.me/" + bot.Username + "?text=" + strings.ReplaceAll(url.QueryEscape(text), "+", "%20")
}
