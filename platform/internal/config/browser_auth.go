package config

import (
	"errors"
	"strings"
)

func (c Config) validateWebAppURLs() error {
	if !httpURL(c.Telegram.WebAppURL, false) || !httpURL(c.Sandbox.MiniAppURL, false) {
		return errors.New("configuration web app URLs must be HTTP URLs")
	}
	if c.Auth.LegacyBrowserOrigins != "" && !strings.HasPrefix(c.Telegram.WebAppURL, "https://") {
		return errors.New("legacy browser origins require an HTTPS web app URL")
	}
	return nil
}
