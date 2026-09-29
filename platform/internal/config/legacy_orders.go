package config

import (
	"errors"
	"strconv"
	"strings"
)

// LegacyOrderBotID comes only from deployment configuration. Zero explicitly
// disables legacy lookup while keeping ordinary sandbox operation available.
func (c Config) LegacyOrderBotID() (int64, error) {
	var configured int64
	if c.Auth.Zitadel.BotID != "" {
		var err error
		configured, err = strconv.ParseInt(c.Auth.Zitadel.BotID, 10, 64)
		if err != nil || configured <= 0 {
			return 0, errors.New("invalid configured legacy order bot identity")
		}
	}
	var tokenID int64
	if prefix, suffix, ok := strings.Cut(c.Telegram.Token.Value(), ":"); ok && suffix != "" &&
		prefix != "" && strings.Trim(prefix, "0123456789") == "" {
		tokenID, _ = strconv.ParseInt(prefix, 10, 64)
	}
	if configured > 0 && tokenID > 0 && configured != tokenID {
		return 0, errors.New("configured legacy order bot identity does not match Telegram token identity")
	}
	if configured > 0 {
		return configured, nil
	}
	if c.Auth.Mode != zitadelMode && tokenID > 0 {
		return tokenID, nil
	}
	return 0, nil
}
