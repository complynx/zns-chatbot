package telegram

import (
	"regexp"
	"strings"

	"github.com/google/uuid"
)

const partnerStartPrefix = "partner_"

var partnerBotUsername = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{4,31}$`)

// PartnerShareURL contains public addressing information, never authentication
// or consent. Empty means the supplied bot identity cannot safely form a URL.
func PartnerShareURL(bot User, botID int64, accountID uuid.UUID) string {
	if botID <= 0 || bot.ID != botID || !bot.IsBot || !partnerBotUsername.MatchString(bot.Username) ||
		accountID == uuid.Nil {
		return ""
	}
	return "https://t.me/" + bot.Username + "?start=" + partnerStartPrefix + strings.ReplaceAll(
		accountID.String(),
		"-",
		"",
	)
}

// PartnerStartCode recognizes only the exact private-chat deep-link command text.
// The caller must independently check chat/sender context and attachment absence.
func PartnerStartCode(text string) *uuid.UUID {
	payload, ok := strings.CutPrefix(text, "/start "+partnerStartPrefix)
	if !ok || len(payload) != 32 {
		return nil
	}
	id, err := uuid.Parse(payload)
	if err != nil || id == uuid.Nil || strings.ReplaceAll(id.String(), "-", "") != payload {
		return nil
	}
	return &id
}
