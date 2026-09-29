package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/bot"
	"github.com/complynx/zns-chatbot/platform/internal/config"
	"github.com/complynx/zns-chatbot/platform/internal/observability"
)

func TestConfigureScriptsExplicitSocketAndDisabledDefault(t *testing.T) {
	t.Parallel()
	runtime, err := observability.New(t.Context(), observability.Config{})
	require.NoError(t, err)
	b := &bot.Bot{}
	closeClient, err := configureBotScripts(b, config.Config{}, runtime)
	require.NoError(t, err)
	closeClient()
	assert.Nil(t, b.Scripts)
	_, err = configureBotScripts(
		b,
		config.Config{Script: config.Script{Enabled: true, Socket: "relative.sock"}},
		runtime,
	)
	require.Error(t, err)
	assert.Nil(t, b.Scripts)
	closeClient, err = configureBotScripts(
		b,
		config.Config{Script: config.Script{Enabled: true, Socket: filepath.Join(t.TempDir(), "script.sock")}},
		runtime,
	)
	require.NoError(t, err)
	defer closeClient()
	assert.NotNil(t, b.Scripts)
}
