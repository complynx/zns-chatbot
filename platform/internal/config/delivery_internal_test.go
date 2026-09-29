package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeliverySettingsRequireTrustedBot(t *testing.T) {
	t.Parallel()
	cfg := defaults(modeApp)
	_, err := cfg.DeliverySettings()
	require.Error(t, err)
	cfg.Auth.Zitadel.BotID = "8123"
	cfg.Telegram.Token = "8123:synthetic"
	settings, err := cfg.DeliverySettings()
	require.NoError(t, err)
	assert.EqualValues(t, 8123, settings.BotID)
	assert.Positive(t, settings.BotInterval)
	assert.Positive(t, settings.ChatInterval)
	assert.Positive(t, settings.Fallback)
	cfg.Telegram.Token = "9123:synthetic"
	_, err = cfg.DeliverySettings()
	require.Error(t, err)
}

func TestDeliverySettingsRejectInvalidPacing(t *testing.T) {
	t.Parallel()
	for _, pacing := range []Delivery{
		{BotInterval: 0, ChatInterval: time.Second, CooldownFallback: time.Second},
		{BotInterval: time.Second, ChatInterval: -time.Second, CooldownFallback: time.Second},
		{BotInterval: time.Second, ChatInterval: time.Second, CooldownFallback: 0},
	} {
		cfg := defaults(modeApp)
		cfg.Auth.Zitadel.BotID = "8123"
		cfg.Delivery = pacing
		_, err := cfg.DeliverySettings()
		require.Error(t, err)
	}
}

func TestDeliveryConfigurationEnvironment(t *testing.T) {
	t.Parallel()
	cfg, err := Load(modeHealth, []byte("env: sandbox\ndelivery:\n  chat_interval: 2s\n"), []string{
		"ZNS_DELIVERY__BOT_INTERVAL=75ms",
		"ZNS_DELIVERY__COOLDOWN_FALLBACK=45s",
	})
	require.NoError(t, err)
	assert.Equal(t, 75*time.Millisecond, cfg.Delivery.BotInterval)
	assert.Equal(t, 2*time.Second, cfg.Delivery.ChatInterval)
	assert.Equal(t, 45*time.Second, cfg.Delivery.CooldownFallback)
	_, err = Load(modeHealth, []byte("env: sandbox\ndelivery:\n  chat_interval: 0s\n"), nil)
	require.ErrorContains(t, err, "delivery.chat_interval")
}
