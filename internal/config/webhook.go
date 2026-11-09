package config

import (
	"errors"
	"net/url"
	"os"
	"slices"
	"strconv"
)

// TelegramWebhookURL resolves the public callback only when management is enabled.
// The Mini App trust origin is operator configuration, never a request Host header.
func (c Config) TelegramWebhookURL() string {
	if !c.Telegram.WebhookEnabled {
		return ""
	}
	if c.Telegram.WebhookURL != "" {
		return c.Telegram.WebhookURL
	}
	if c.MiniAppOrigin == "" {
		return ""
	}
	return c.MiniAppOrigin + "/telegram/webhook"
}

func (c Config) validateWebhook() error {
	if !c.Telegram.WebhookEnabled {
		return nil
	}
	endpoint := c.TelegramWebhookURL()
	u, err := url.Parse(endpoint)
	if err != nil || len(endpoint) > 2048 || u.Scheme != schemeHTTPS || u.Hostname() == "" || u.User != nil ||
		u.Path != "/telegram/webhook" || u.RawPath != "" || u.RawQuery != "" ||
		u.ForceQuery || u.Fragment != "" || u.Opaque != "" ||
		!slices.Contains([]string{"", "443", "80", "88", "8443"}, u.Port()) {
		return errors.New("enabled Telegram webhook requires a public HTTPS origin or exact webhook URL")
	}
	return nil
}

func applyWebhookEnvironment(c *Config) error {
	value, ok := os.LookupEnv("ZNS_CHATBOT_TELEGRAM_WEBHOOK_ENABLED")
	if !ok {
		return nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return errors.New("invalid ZNS_CHATBOT_TELEGRAM_WEBHOOK_ENABLED")
	}
	c.Telegram.WebhookEnabled = parsed
	return nil
}
