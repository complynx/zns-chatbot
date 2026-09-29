package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestBrowserOriginConfigurationValidationAndPrecedence(t *testing.T) {
	t.Parallel()
	yaml := []byte(
		"auth:\n  legacy_browser_origins: https://yaml.example\ntelegram:\n  web_app_url: https://bot.example/miniapp/\n",
	)
	environment := append(botEnvironment(), "ZNS_AUTH__LEGACY_BROWSER_ORIGINS=https://env.example")
	result, err := config.Load("bot", yaml, environment)
	require.NoError(t, err)
	assert.Equal(t, "https://env.example", result.Auth.LegacyBrowserOrigins)
	for _, raw := range []string{"*", "https://example.com https://example.com", "https://example.com/"} {
		_, err = config.Load("bot", yaml, append(botEnvironment(), "ZNS_AUTH__LEGACY_BROWSER_ORIGINS="+raw))
		require.Error(t, err)
	}
	_, err = config.Load("bot", nil, environment)
	require.Error(t, err, "cross-site cookies require an HTTPS public URL")
}
