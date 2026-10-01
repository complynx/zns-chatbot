package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/delivery"
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
		pacing.UncertaintyRetryBase = delivery.DefaultUncertaintyRetryBase
		cfg.Delivery = pacing
		_, err := cfg.DeliverySettings()
		require.Error(t, err)
	}
}

func TestDeliveryDefaultRetryBaseIsIndependent(t *testing.T) {
	t.Parallel()
	cfg, err := Load(modeHealth, []byte("env: sandbox\n"), nil)
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, cfg.Delivery.UncertaintyRetryBase)
	assert.Equal(t, 30*time.Second, cfg.Delivery.CooldownFallback)
	cfg.Auth.Zitadel.BotID = "8123"
	cfg.Telegram.Token = "8123:synthetic"
	settings, err := cfg.DeliverySettings()
	require.NoError(t, err)
	assert.Equal(t, 5*time.Second, settings.UncertaintyRetryBaseOrDefault())
	assert.Equal(t, 30*time.Second, settings.Fallback)
}

func TestDeliveryRetryBaseConfigurationOverrides(t *testing.T) {
	t.Parallel()
	for _, sample := range []struct {
		name    string
		environ []string
		want    time.Duration
	}{
		{"yaml", nil, 7 * time.Second},
		{"environment", []string{"ZNS_DELIVERY__UNCERTAINTY_RETRY_BASE=9s"}, 9 * time.Second},
	} {
		t.Run(sample.name, func(t *testing.T) {
			t.Parallel()
			cfg, err := Load(
				modeHealth,
				[]byte("env: sandbox\ndelivery:\n  uncertainty_retry_base: 7s\n  cooldown_fallback: 45s\n"),
				sample.environ,
			)
			require.NoError(t, err)
			assert.Equal(t, sample.want, cfg.Delivery.UncertaintyRetryBase)
			assert.Equal(t, 45*time.Second, cfg.Delivery.CooldownFallback)
			cfg.Auth.Zitadel.BotID = "8123"
			cfg.Telegram.Token = "8123:synthetic"
			settings, err := cfg.DeliverySettings()
			require.NoError(t, err)
			assert.Equal(t, sample.want, settings.UncertaintyRetryBase)
			assert.Equal(t, sample.want, settings.UncertaintyRetryBaseOrDefault())
			assert.Equal(t, 45*time.Second, settings.Fallback)
		})
	}
}

func TestDeliveryRetryBaseRejectsNonpositiveConfiguration(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"0s", "-1s"} {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			_, err := Load(modeHealth, []byte("env: sandbox\ndelivery:\n  uncertainty_retry_base: "+value+"\n"), nil)
			require.ErrorContains(t, err, "delivery.uncertainty_retry_base")
			_, err = Load(
				modeHealth,
				[]byte("env: sandbox\n"),
				[]string{"ZNS_DELIVERY__UNCERTAINTY_RETRY_BASE=" + value},
			)
			require.ErrorContains(t, err, "delivery.uncertainty_retry_base")
			base, err := time.ParseDuration(value)
			require.NoError(t, err)
			cfg := defaults(modeApp)
			cfg.Auth.Zitadel.BotID = "8123"
			cfg.Telegram.Token = "8123:synthetic"
			cfg.Delivery.UncertaintyRetryBase = base
			_, err = cfg.DeliverySettings()
			require.ErrorIs(t, err, delivery.ErrSettings)
		})
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
