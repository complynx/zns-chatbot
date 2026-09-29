package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestAssistantQuotaConfiguration(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load("bot", nil, botEnvironment())
	require.NoError(t, err)
	assert.Equal(t, 50, cfg.Model.AssistantDailyLimit)
	data := []byte("model:\n  assistant_daily_limit: 7\n")
	cfg, err = config.Load("bot", data, botEnvironment())
	require.NoError(t, err)
	assert.Equal(t, 7, cfg.Model.AssistantDailyLimit)
	cfg, err = config.Load("bot", data, append(botEnvironment(), "ZNS_MODEL__ASSISTANT_DAILY_LIMIT=9"))
	require.NoError(t, err)
	assert.Equal(t, 9, cfg.Model.AssistantDailyLimit)
	for _, value := range []string{"0", "-1", "10001", "no", "9999999999999999999999999"} {
		_, err = config.Load("bot", nil, append(botEnvironment(), "ZNS_MODEL__ASSISTANT_DAILY_LIMIT="+value))
		require.Error(t, err)
	}
}
