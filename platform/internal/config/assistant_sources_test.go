package config_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/complynx/zns-chatbot/platform/internal/config"
)

func TestAssistantSourcesSecretConfig(t *testing.T) {
	t.Parallel()
	cfg, err := config.Load(
		"health",
		nil,
		[]string{
			"ZNS_ASSISTANT_SOURCES__ABOUT_DOCUMENT=synthetic",
			"ZNS_ASSISTANT_SOURCES__CREDENTIALS=credential-canary",
			"ZNS_ASSISTANT_SOURCES__STATIC_QA_PATH=/qa.yaml",
		},
	)
	require.NoError(t, err)
	assert.Equal(t, "synthetic", cfg.AssistantSources.AboutDocument)
	assert.Equal(t, "credential-canary", cfg.AssistantSources.Credentials.Value())
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "credential-canary")
	_, err = config.Load("health", nil, []string{"ZNS_ASSISTANT_SOURCES__ABOUT_DOCUMENT=synthetic"})
	require.Error(t, err)
}
