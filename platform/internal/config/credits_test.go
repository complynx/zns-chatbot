package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestCreditsRemoteGateDoesNotBlockMaintenance(t *testing.T) {
	t.Parallel()
	environment := append(botEnvironment(), "ZNS_CREDITS__ENFORCE=true")
	for _, command := range []string{"health", "migrate"} {
		_, err := config.Load(command, nil, environment)
		require.NoError(t, err)
	}
	for _, command := range []string{"bot", "api"} {
		_, err := config.Load(command, nil, environment)
		require.ErrorContains(t, err, "trusted direct model attribution")
	}
}
