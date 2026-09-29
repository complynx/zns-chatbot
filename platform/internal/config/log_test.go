package config_test

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestLogLevels(t *testing.T) {
	t.Parallel()
	levels := map[string]slog.Level{
		"trace":   -8,
		"debug":   slog.LevelDebug,
		"info":    slog.LevelInfo,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for name, expected := range levels {
		cfg, err := config.Load("health", []byte("log: {level: "+name+"}"), nil)
		require.NoError(t, err)
		actual, err := cfg.Log.SlogLevel()
		require.NoError(t, err)
		assert.Equal(t, expected, actual)
	}
	cfg, err := config.Load("health", []byte("log: {level: error}"), []string{"ZNS_LOG__LEVEL=debug"})
	require.NoError(t, err)
	assert.Equal(t, "debug", cfg.Log.Level)
	_, err = config.Load("health", []byte("log: {level: private-invalid}"), nil)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "private-invalid")
}
