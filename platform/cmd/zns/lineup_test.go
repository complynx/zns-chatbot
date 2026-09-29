package main

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestConfigureBotLineup(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	instance := &bot.Bot{Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	path := filepath.Join(t.TempDir(), "private-lineup.csv")
	cfg := config.Config{Lineup: config.Lineup{CSV: path, EventYear: 2026, Timezone: "Europe/Minsk"}}
	configureBotLineup(instance, cfg)
	assert.Nil(t, instance.Lineup)
	assert.Contains(t, logs.String(), "DJ lineup unavailable")
	assert.NotContains(t, logs.String(), path)
	require.NoError(t, os.WriteFile(path, []byte("time,\"Fri, 25.09\"\nroom,Main\n22:00,DJ\n"), 0o600))
	configureBotLineup(instance, cfg)
	require.NotNil(t, instance.Lineup)
	now := time.Date(2026, time.September, 25, 19, 0, 0, 0, time.UTC)
	assert.Equal(t, "available", instance.Lineup.Snapshot(now).Reads[0].Status)
	// Source replacement does not silently change a running process's snapshot.
	require.NoError(t, os.WriteFile(path, nil, 0o600))
	assert.Equal(t, "available", instance.Lineup.Snapshot(now).Reads[0].Status)
	configureBotLineup(instance, cfg)
	assert.Equal(t, "no_current_party", instance.Lineup.Snapshot(now).Reads[0].Status)
}
