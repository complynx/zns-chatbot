package config_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestScriptConfigurationRequiresExplicitAbsoluteSocket(t *testing.T) {
	t.Parallel()
	_, err := config.Load("health", nil, []string{"ZNS_SCRIPT__ENABLED=true", "ZNS_SCRIPT__SOCKET=relative.sock"})
	require.Error(t, err)
	socket := filepath.Join(t.TempDir(), "worker.sock")
	cfg, err := config.Load("health", nil, []string{"ZNS_SCRIPT__ENABLED=true", "ZNS_SCRIPT__SOCKET=" + socket})
	require.NoError(t, err)
	assert.True(t, cfg.Script.Enabled)
	assert.Equal(t, socket, cfg.Script.Socket)
	cfg, err = config.Load("health", nil, nil)
	require.NoError(t, err)
	assert.False(t, cfg.Script.Enabled)
}
