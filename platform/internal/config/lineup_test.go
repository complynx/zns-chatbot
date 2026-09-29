package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestLineupConfiguration(t *testing.T) {
	t.Parallel()
	environment := append(
		botEnvironment(),
		"LINEUP_CSV=lineup.csv",
		"LINEUP_EVENT_YEAR=2026",
		"LINEUP_TIMEZONE=Europe/Minsk",
	)
	cfg, err := config.Load("bot", nil, environment)
	require.NoError(t, err)
	assert.Equal(t, config.Lineup{CSV: "lineup.csv", EventYear: 2026, Timezone: "Europe/Minsk"}, cfg.Lineup)
	for _, override := range []string{
		"ZNS_LINEUP__CSV=", "ZNS_LINEUP__EVENT_YEAR=0", "ZNS_LINEUP__EVENT_YEAR=10000",
		"ZNS_LINEUP__TIMEZONE=", "ZNS_LINEUP__TIMEZONE=Local", "ZNS_LINEUP__TIMEZONE=invalid/zone",
	} {
		_, err = config.Load("bot", nil, append(environment, override))
		require.Error(t, err)
	}
}
