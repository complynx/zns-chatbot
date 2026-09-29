package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestActiveEventConfiguration(t *testing.T) {
	t.Parallel()
	defaults, err := config.Load("bot", nil, botEnvironment())
	require.NoError(t, err)
	assert.Equal(t, "sandbox-festival", defaults.Orders.ActiveEvent)
	cfg, err := config.Load(
		"bot",
		[]byte("orders:\n  active_event: yaml-event\n"),
		append(botEnvironment(), "ZNS_ORDERS__ACTIVE_EVENT=grodno_26"),
	)
	require.NoError(t, err)
	assert.Equal(t, "grodno_26", cfg.Orders.ActiveEvent)
	_, err = config.Load("bot", nil, append(botEnvironment(), "ZNS_ORDERS__ACTIVE_EVENT=bad/event"))
	require.ErrorContains(t, err, "orders.active_event")
}
