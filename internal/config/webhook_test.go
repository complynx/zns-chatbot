package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/internal/config"
)

func TestTelegramWebhookConfiguration(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, origin, override, expected string
		enabled, invalid                 bool
	}{
		{name: "disabled leaves existing webhook", origin: "https://app.example.invalid"},
		{name: "derived origin", enabled: true, origin: "https://app.example.invalid",
			expected: "https://app.example.invalid/telegram/webhook"},
		{name: "explicit without mini app", enabled: true, override: "https://bot.example.invalid/telegram/webhook",
			expected: "https://bot.example.invalid/telegram/webhook"},
		{name: "missing origin", enabled: true, invalid: true},
		{name: "HTTP callback rejected", enabled: true, override: "http://bot.invalid/telegram/webhook", invalid: true},
		{name: "wrong path", enabled: true, override: "https://bot.invalid/app/", invalid: true},
		{name: "query rejected", enabled: true, override: "https://bot.invalid/telegram/webhook?secret=x", invalid: true},
		{name: "userinfo rejected", enabled: true, override: "https://x:y@bot.invalid/telegram/webhook", invalid: true},
		{name: "unsupported port", enabled: true, override: "https://bot.invalid:8080/telegram/webhook", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, err := config.Load(configFile(t, ""))
			require.NoError(t, err)
			c.MiniAppOrigin = tc.origin
			c.Telegram.WebhookEnabled, c.Telegram.WebhookURL = tc.enabled, tc.override
			if tc.invalid {
				require.ErrorContains(t, c.Validate(), "webhook")
				return
			}
			require.NoError(t, c.Validate())
			require.Equal(t, tc.expected, c.TelegramWebhookURL())
		})
	}
}

func TestTelegramWebhookEnvironment(t *testing.T) {
	t.Setenv("ZNS_CHATBOT_TELEGRAM_WEBHOOK_ENABLED", "true")
	t.Setenv("ZNS_CHATBOT_TELEGRAM_WEBHOOK_URL", "https://bot.invalid/telegram/webhook")
	c, err := config.Load(configFile(t, ""))
	require.NoError(t, err)
	require.Equal(t, "https://bot.invalid/telegram/webhook", c.TelegramWebhookURL())
	t.Setenv("ZNS_CHATBOT_TELEGRAM_WEBHOOK_ENABLED", "invalid-private-value")
	_, err = config.Load(configFile(t, ""))
	require.ErrorContains(t, err, "ZNS_CHATBOT_TELEGRAM_WEBHOOK_ENABLED")
	require.NotContains(t, err.Error(), "private-value")
}
